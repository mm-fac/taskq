package main

import (
	"fmt"
	"strconv"
)

// rm.go implements `taskq rm <n>`: it deletes the addressed task — open or
// completed — from the task file and prints the removed line to stdout prefixed
// by `removed: ` (requirements.md, "Commands"). Unlike the number-prefixed
// output of the other mutating commands, the deleted task no longer has a line
// number in the rewritten file, so `removed: ` is used instead.
//
// `rm` addresses a task by number, so it loads via the store's LoadForMutation:
// a missing task file is an I/O error (exit 2), and a number out of range or
// landing on a malformed line is a usage error (exit 1). The write goes through
// the store's atomic rewrite, preserving the remaining malformed lines
// byte-for-byte in place and leaving exactly one trailing newline; deleting the
// only task yields a zero-byte file.

func init() {
	register(command{
		name:    "rm",
		summary: "delete a task (rm <n>)",
		run:     runRm,
	})
}

// runRm parses the single task-number argument, resolves it against the task
// file, removes the addressed task from the line list, rewrites the file
// atomically, and prints the deleted line prefixed by `removed: `.
func runRm(ctx *cmdContext) error {
	// rm takes exactly one positional argument: the 1-based task number.
	if len(ctx.args) != 1 {
		return usagef("rm: want exactly one task number")
	}
	num, convErr := strconv.Atoi(ctx.args[0])
	if convErr != nil {
		return usagef("rm: invalid task number %q", ctx.args[0])
	}

	// Number-addressing mutation: a missing file is an I/O error (exit 2).
	tasks, malformed, err := LoadForMutation(ctx.filePath)
	if err != nil {
		return err
	}
	ctx.noteMalformed(malformed)

	// Resolve maps the number to a real task line; out-of-range or malformed
	// targets are usage errors (exit 1). A completed task resolves fine — rm
	// deletes open and completed tasks alike.
	idx, err := Resolve(tasks, num)
	if err != nil {
		return err
	}
	removed := tasks[idx]

	// Drop the addressed line; every other line (tasks and preserved malformed
	// lines) keeps its position, so the atomic rewrite disturbs nothing else.
	tasks = append(tasks[:idx], tasks[idx+1:]...)

	if err := Save(ctx.filePath, tasks); err != nil {
		return err
	}
	fmt.Fprintf(ctx.stdout, "removed: %s\n", removed.Render())
	return nil
}
