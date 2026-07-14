package main

import (
	"fmt"
	"strings"
)

// add.go implements `taskq add <text...>`: it appends one task to the task
// file. The arguments are joined with single spaces; a leading `(A) `–`(Z) `
// token is peeled off as the priority (and only that token — the rest of the
// user text is never scanned for a completion marker or creation date); today's
// injected date is prepended as the creation date; and the resulting line is
// appended in file order. The added line is printed to stdout prefixed by its
// new 1-based line number. Empty text is a usage error and writes nothing. A
// missing task file is created (add uses Load, for which a missing file is an
// empty list, not an error). The write goes through the store's atomic rewrite,
// so exactly one trailing newline is left and existing malformed lines are
// preserved byte-for-byte.

func init() {
	register(command{
		name:    "add",
		summary: "append a task (add <text...>)",
		run:     runAdd,
	})
}

// runAdd builds the new task line from ctx.args and appends it. It joins the
// arguments with single spaces, extracts a leading priority per the grammar,
// prepends ctx.todayStr as the creation date, and prints the added line with
// its 1-based line number. Empty joined text (no args, or nothing but a
// consumed priority / whitespace) is a usage error reported before any load or
// write, so nothing is touched.
func runAdd(ctx *cmdContext) error {
	joined := strings.Join(ctx.args, " ")

	// Peel a single leading "(A) "–"(Z) " token as the priority, matching the
	// grammar's priority rule (uppercase letter, closing paren, one space).
	// Everything after it is verbatim task text; unlike ParseLine we never scan
	// the user's text for a completion marker or an embedded creation date.
	var priority byte
	text := joined
	if len(joined) >= 4 && joined[0] == '(' && joined[2] == ')' && joined[3] == ' ' &&
		joined[1] >= 'A' && joined[1] <= 'Z' {
		priority = joined[1]
		text = joined[4:]
	}

	// Empty (or whitespace-only) text is a usage error: a well-formed task must
	// carry non-empty text. Checked before loading so add writes nothing.
	if strings.TrimSpace(text) == "" {
		return usagef("add: empty task text")
	}

	// A missing file is not an error for add: Load yields an empty list and Save
	// creates the file.
	tasks, malformed, err := Load(ctx.filePath)
	if err != nil {
		return err
	}
	ctx.noteMalformed(malformed)

	task := Task{
		Priority:     priority,
		CreationDate: ctx.todayStr,
		Text:         text,
	}
	tasks = append(tasks, task)
	if err := Save(ctx.filePath, tasks); err != nil {
		return err
	}

	// Identity is the 1-based line number over ALL lines; the appended task is
	// the last line, so its number is the new line count.
	num := len(tasks)
	fmt.Fprintf(ctx.stdout, "%d %s\n", num, task.Render())
	return nil
}
