package main

import (
	"fmt"
	"strconv"
	"strings"
)

// due.go implements `taskq due <n> <YYYY-MM-DD|none>`: it sets the addressed
// task's `due:` token — replacing an existing one in place, or appending
// `due:<date>` at the end of the text when there is none — or removes it when
// the date argument is `none` (requirements.md, "Commands"). When the task
// carries multiple pre-existing `due:` tokens the FIRST is replaced (or removed
// for `none`) and every remaining `due:` token is dropped (decided). The
// resulting line is printed to stdout prefixed by its 1-based number.
//
// A `due:` token is a whitespace-delimited field of the task text beginning
// with `due:`; this is a detail-level choice, matching the token marker
// literally so that `none` reliably clears a stray `due:` marker even if its
// value is not a valid date.
//
// `due` addresses a task by number, so it loads via the store's
// LoadForMutation: a missing task file is an I/O error (exit 2), and a number
// out of range or landing on a malformed line is a usage error (exit 1). The
// date argument must be a calendar-valid, zero-padded YYYY-MM-DD (or `none`),
// else it is a usage error, validated before the file is touched so nothing is
// written. Running `due` on a completed task is a usage error: completed tasks
// carry no due date to manage. The write goes through the store's atomic
// rewrite, preserving malformed lines byte-for-byte and leaving exactly one
// trailing newline.

func init() {
	register(command{
		name:    "due",
		summary: "set or clear a task's due date (due <n> <YYYY-MM-DD|none>)",
		run:     runDue,
	})
}

// runDue parses the task-number and date arguments, resolves the number against
// the task file, and sets or clears the addressed task's `due:` token. A
// completed task is rejected as a usage error; otherwise the token is
// reconciled per applyDue and the file is rewritten atomically before the
// resulting line is printed prefixed by its number.
func runDue(ctx *cmdContext) error {
	// due takes exactly two positional arguments: the 1-based task number and
	// the target date (a zero-padded, calendar-valid YYYY-MM-DD, or `none`).
	if len(ctx.args) != 2 {
		return usagef("due: want a task number and a date (YYYY-MM-DD or none)")
	}
	num, convErr := strconv.Atoi(ctx.args[0])
	if convErr != nil {
		return usagef("due: invalid task number %q", ctx.args[0])
	}

	// Parse the date argument before touching the filesystem: a bad value is a
	// usage error regardless of the file's state, and nothing is written. An
	// empty date signals removal (the `none` case).
	date, err := parseDueArg(ctx.args[1])
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

	// A completed task carries no due date to manage, so setting or clearing one
	// is a usage error.
	if t.Completed {
		return usagef("due: task %d is completed and carries no due date", num)
	}

	// Reconcile the due: token(s) and rewrite atomically.
	t.Text = applyDue(t.Text, date)
	tasks[idx] = t

	if err := Save(ctx.filePath, tasks); err != nil {
		return err
	}
	fmt.Fprintf(ctx.stdout, "%d %s\n", num, t.Render())
	return nil
}

// parseDueArg interprets the date argument: a calendar-valid, zero-padded
// YYYY-MM-DD is returned as-is, and `none` (case-insensitive, matching the
// sibling `pri` command) yields "" to signal removal. Any other value is a
// usage error (exit 1).
func parseDueArg(arg string) (string, error) {
	if strings.EqualFold(arg, "none") {
		return "", nil
	}
	if validDate(arg) {
		return arg, nil
	}
	return "", usagef("due: invalid date %q: want a zero-padded YYYY-MM-DD or none", arg)
}

// applyDue reconciles the `due:` token(s) in a task's text. When date is
// non-empty it becomes the value of the first `due:` token — replacing it in
// place if one exists, or appending `due:<date>` at the end otherwise. When
// date is "" (the `none` case) the first `due:` token is removed. In both cases
// any further `due:` tokens are dropped, so at most one survives. Tokens are the
// whitespace-delimited fields of the text; non-due fields are preserved in order
// and rejoined with single spaces (the grammar's field separator).
func applyDue(text, date string) string {
	fields := strings.Fields(text)
	out := make([]string, 0, len(fields)+1)
	seenDue := false
	for _, f := range fields {
		if strings.HasPrefix(f, "due:") {
			if !seenDue {
				seenDue = true
				if date != "" {
					out = append(out, "due:"+date) // replace the first in place
				}
				// date == "" removes the first; either way drop the rest.
			}
			continue
		}
		out = append(out, f)
	}
	if !seenDue && date != "" {
		out = append(out, "due:"+date) // no existing token: append at the end
	}
	return strings.Join(out, " ")
}
