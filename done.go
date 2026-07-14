package main

import (
	"fmt"
	"strconv"
)

// done.go implements `taskq done <n>`: it marks an open task complete by
// prefixing `x <today> ` and dropping its priority, so the rewritten line is
// `x <completion-date> [creation-date] text` (priority is dropped on
// completion — requirements.md, "The task file"). The affected line is printed
// to stdout prefixed by its 1-based number.
//
// `done` addresses a task by number, so it loads via the store's
// LoadForMutation: a missing task file is an I/O error (exit 2), and a number
// out of range or landing on a malformed line is a usage error (exit 1). The
// write goes through the store's atomic rewrite, preserving malformed lines
// byte-for-byte and leaving exactly one trailing newline.
//
// Running `done` on an ALREADY-completed task is an idempotent no-op: the line
// is left unchanged, printed unchanged prefixed by its number, exit 0, with a
// `taskq: task N already done` note on stderr (requirements.md, "Commands").

func init() {
	register(command{
		name:    "done",
		summary: "mark a task complete (done <n>)",
		run:     runDone,
	})
}

// runDone parses the single task-number argument, resolves it against the task
// file, and completes the addressed task. An already-completed task is a no-op
// with the decided stderr note; a fresh completion sets the completion marker,
// drops the priority, and rewrites the file atomically before printing the
// resulting line.
func runDone(ctx *cmdContext) error {
	// done takes exactly one positional argument: the 1-based task number.
	if len(ctx.args) != 1 {
		return usagef("done: want exactly one task number")
	}
	num, convErr := strconv.Atoi(ctx.args[0])
	if convErr != nil {
		return usagef("done: invalid task number %q", ctx.args[0])
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

	// Idempotent no-op: an already-completed task is left byte-for-byte
	// unchanged, printed unchanged, exit 0, with the decided stderr note.
	if t.Completed {
		fmt.Fprintf(ctx.stderr, "taskq: task %d already done\n", num)
		fmt.Fprintf(ctx.stdout, "%d %s\n", num, t.Render())
		return nil
	}

	// Complete the task: prefix `x <today> ` and drop the priority.
	t.Completed = true
	t.CompletionDate = ctx.todayStr
	t.Priority = 0
	tasks[idx] = t

	if err := Save(ctx.filePath, tasks); err != nil {
		return err
	}
	fmt.Fprintf(ctx.stdout, "%d %s\n", num, t.Render())
	return nil
}
