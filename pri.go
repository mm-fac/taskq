package main

import (
	"fmt"
	"strconv"
	"strings"
)

// pri.go implements `taskq pri <n> <A-Z|none>`: it sets the addressed task's
// priority — replacing any existing one — or removes it when the priority
// argument is `none` (requirements.md, "Commands"). A lowercase letter is
// accepted and uppercased (so `b` sets `(B)`); an argument that is neither a
// single A–Z letter nor `none` is a usage error. The resulting line is printed
// to stdout prefixed by its 1-based number.
//
// `pri` addresses a task by number, so it loads via the store's
// LoadForMutation: a missing task file is an I/O error (exit 2), and a number
// out of range or landing on a malformed line is a usage error (exit 1).
// Running `pri` on a completed task is a usage error: completed tasks carry no
// priority (it is dropped on completion). The write goes through the store's
// atomic rewrite, preserving malformed lines byte-for-byte and leaving exactly
// one trailing newline.

func init() {
	register(command{
		name:    "pri",
		summary: "set or clear a task's priority (pri <n> <A-Z|none>)",
		run:     runPri,
	})
}

// runPri parses the task-number and priority arguments, resolves the number
// against the task file, and sets or clears the addressed task's priority. A
// completed task is rejected as a usage error; otherwise the priority is
// replaced (or removed) and the file is rewritten atomically before the
// resulting line is printed prefixed by its number.
func runPri(ctx *cmdContext) error {
	// pri takes exactly two positional arguments: the 1-based task number and
	// the target priority (a single A-Z letter, case-insensitive, or `none`).
	if len(ctx.args) != 2 {
		return usagef("pri: want a task number and a priority (A-Z or none)")
	}
	num, convErr := strconv.Atoi(ctx.args[0])
	if convErr != nil {
		return usagef("pri: invalid task number %q", ctx.args[0])
	}

	// Parse the priority argument before touching the filesystem: a bad value is
	// a usage error regardless of the file's state, and nothing is written.
	priority, err := parsePriorityArg(ctx.args[1])
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

	// A completed task carries no priority (dropped on completion), so setting or
	// clearing one is a usage error.
	if t.Completed {
		return usagef("pri: task %d is completed and carries no priority", num)
	}

	// Replace the priority (0 clears it for `none`) and rewrite atomically.
	t.Priority = priority
	tasks[idx] = t

	if err := Save(ctx.filePath, tasks); err != nil {
		return err
	}
	fmt.Fprintf(ctx.stdout, "%d %s\n", num, t.Render())
	return nil
}

// parsePriorityArg interprets the priority argument: a single letter (either
// case) yields its uppercase priority byte, and `none` (case-insensitive)
// yields 0 to clear the priority. Any other value is a usage error (exit 1).
func parsePriorityArg(arg string) (byte, error) {
	if len(arg) == 1 {
		c := arg[0]
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A' // uppercase the lowercase letter (decided)
		}
		if c >= 'A' && c <= 'Z' {
			return c, nil
		}
	}
	if strings.EqualFold(arg, "none") {
		return 0, nil
	}
	return 0, usagef("pri: invalid priority %q: want a single A-Z letter or none", arg)
}
