package main

import (
	"fmt"
	"strconv"
	"strings"
)

// due.go implements `taskq due <n> <YYYY-MM-DD|none>`: it sets the addressed
// task's `due:` token — replacing an existing one in place, or appending
// `due:<date>` at the end of the text when there is none — or removes it when
// the argument is `none` (requirements.md, "Commands"). When the task carries
// several pre-existing `due:` tokens the FIRST is replaced (or removed for
// `none`) and the remaining `due:` tokens are dropped (decided). The resulting
// line is printed to stdout prefixed by its 1-based number.
//
// `due` addresses a task by number, so it loads via the store's
// LoadForMutation: a missing task file is an I/O error (exit 2), and a number
// out of range or landing on a malformed line is a usage error (exit 1).
// Running `due` on a completed task is a usage error (completed tasks are not
// re-dated), as is a date argument that is neither a calendar-valid,
// zero-padded YYYY-MM-DD nor `none`. Per the owner decision, a removal whose
// `due:` token(s) are the task's entire text — which would leave the text
// empty — is also a usage error: nothing is written, since empty task text is
// never a tool output. The write goes through the store's atomic rewrite,
// preserving malformed lines byte-for-byte and leaving exactly one trailing
// newline.

func init() {
	register(command{
		name:    "due",
		summary: "set or clear a task's due date (due <n> <YYYY-MM-DD|none>)",
		run:     runDue,
	})
}

// runDue parses the task-number and date arguments, resolves the number against
// the task file, and sets, replaces, appends, or removes the addressed task's
// `due:` token. A completed task is rejected as a usage error, as is a removal
// that would empty the task text; otherwise the text is rewritten and the file
// is saved atomically before the resulting line is printed prefixed by its
// number.
func runDue(ctx *cmdContext) error {
	// due takes exactly two positional arguments: the 1-based task number and the
	// target date (a zero-padded, calendar-valid YYYY-MM-DD, or `none`).
	if len(ctx.args) != 2 {
		return usagef("due: want a task number and a date (YYYY-MM-DD or none)")
	}
	num, convErr := strconv.Atoi(ctx.args[0])
	if convErr != nil {
		return usagef("due: invalid task number %q", ctx.args[0])
	}

	// Parse the date argument before touching the filesystem: a bad value is a
	// usage error regardless of the file's state, and nothing is written.
	date, remove, err := parseDueArg(ctx.args[1])
	if err != nil {
		return err
	}

	// Number-addressing mutation: a missing file is an I/O error (exit 2).
	tasks, malformed, err := LoadForMutation(ctx.filePath)
	if err != nil {
		return err
	}
	ctx.noteMalformed(malformed)

	// Resolve maps the number to a real task line; out-of-range or malformed
	// targets are usage errors (exit 1).
	idx, err := Resolve(tasks, num)
	if err != nil {
		return err
	}
	t := tasks[idx]

	// A completed task is not re-dated: setting or clearing its due date is a
	// usage error.
	if t.Completed {
		return usagef("due: task %d is completed", num)
	}

	// Rewrite the text's due tokens. A removal that would leave the text empty is
	// a usage error (owner decision): empty task text is preserved input, never a
	// tool output, so nothing is written.
	newText, ok := applyDue(t.Text, date, remove)
	if !ok {
		return usagef("due: task %d would have empty text after removing due:", num)
	}
	t.Text = newText
	tasks[idx] = t

	if err := Save(ctx.filePath, tasks); err != nil {
		return err
	}
	fmt.Fprintf(ctx.stdout, "%d %s\n", num, t.Render())
	return nil
}

// parseDueArg interprets the date argument: `none` (case-insensitive) requests
// removal, and any other value must be a zero-padded, calendar-valid
// YYYY-MM-DD date. It returns the date (empty when removing), whether removal
// was requested, and a usage error (exit 1) for any other value.
func parseDueArg(arg string) (date string, remove bool, err error) {
	if strings.EqualFold(arg, "none") {
		return "", true, nil
	}
	if validDate(arg) {
		return arg, false, nil
	}
	return "", false, usagef("due: invalid date %q: want YYYY-MM-DD or none", arg)
}

// applyDue rewrites the `due:` tokens in a task's text. A `due:` token is a
// space-separated run beginning with "due:". When remove is false the first
// such token is replaced in place with "due:<date>" (or "due:<date>" is
// appended at the end of the text when there is none) and any further `due:`
// tokens are dropped. When remove is true every `due:` token is dropped. Text
// outside the affected tokens — including its spacing — is left untouched;
// removing a token also consumes one adjacent separating space so no double or
// dangling space is left behind.
//
// applyDue returns the rewritten text and ok=true, except that a removal
// leaving no non-whitespace text returns ok=false, signalling the caller to
// reject the mutation as a usage error (owner decision: a due removal must
// never empty a task).
func applyDue(text, date string, remove bool) (string, bool) {
	spans := dueTokenSpans(text)

	// No pre-existing due token: `none` is a no-op, a date appends at the end.
	if len(spans) == 0 {
		if remove {
			return text, true
		}
		return text + " due:" + date, true
	}

	// Build a keep mask over the bytes so overlapping adjacent-space removals are
	// naturally idempotent. Every dropped token clears its own bytes and one
	// adjacent separating space (the following space if present, else the
	// preceding one), collapsing what would otherwise be a double or dangling
	// space. The first token, when replacing, is left in place and substituted
	// during reconstruction below.
	keep := make([]bool, len(text))
	for i := range keep {
		keep[i] = true
	}
	drop := func(s, e int) {
		for i := s; i < e; i++ {
			keep[i] = false
		}
		if e < len(text) && text[e] == ' ' {
			keep[e] = false
		} else if s > 0 && text[s-1] == ' ' {
			keep[s-1] = false
		}
	}

	replaceFirst := !remove
	for i, sp := range spans {
		if i == 0 && replaceFirst {
			continue // kept in place; substituted during reconstruction
		}
		drop(sp[0], sp[1])
	}

	var b strings.Builder
	i := 0
	for i < len(text) {
		if replaceFirst && i == spans[0][0] {
			b.WriteString("due:" + date)
			i = spans[0][1]
			continue
		}
		if keep[i] {
			b.WriteByte(text[i])
		}
		i++
	}
	result := b.String()

	// A removal that erased all real text (its due token(s) were the whole text)
	// is rejected upstream; report it as not-ok.
	if remove && strings.TrimSpace(result) == "" {
		return "", false
	}
	return result, true
}

// dueTokenSpans returns the [start,end) byte spans of the space-separated
// tokens in text that begin with "due:", in order. The separator is the ASCII
// space, matching the fixed space-separated grammar (requirements.md, "The task
// file"), so tabs or other bytes stay part of a token.
func dueTokenSpans(text string) [][2]int {
	var spans [][2]int
	i := 0
	for i < len(text) {
		for i < len(text) && text[i] == ' ' {
			i++
		}
		start := i
		for i < len(text) && text[i] != ' ' {
			i++
		}
		if start < i && strings.HasPrefix(text[start:i], "due:") {
			spans = append(spans, [2]int{start, i})
		}
	}
	return spans
}
