package main

import (
	"fmt"
	"strconv"
	"strings"
)

// edit.go implements `taskq edit <n> <text...>`: it replaces only the text
// field of the addressed open task with the remaining arguments joined by
// single spaces. The parsed priority and creation date (including their
// absence) are left untouched. Completed tasks cannot be edited.
//
// A replacement must contain non-whitespace text and must not begin with an
// uppercase `(A) `-`(Z) ` priority prefix. Priority changes belong to `pri`;
// project, context, and due-looking tokens elsewhere in the replacement are
// ordinary text and are stored without validation. The rewrite uses the
// store's atomic Save path, preserving all other lines, including malformed
// ones, and leaving exactly one trailing newline.

func init() {
	register(command{
		name:    "edit",
		summary: "replace a task's text (edit <n> <text...>)",
		run:     runEdit,
	})
}

// runEdit validates and joins the replacement, resolves the 1-based line
// number against the task file, changes only Task.Text, saves atomically, and
// prints the resulting line prefixed by its number.
func runEdit(ctx *cmdContext) error {
	if len(ctx.args) < 2 {
		return usagef("edit: want a task number and replacement text")
	}

	num, err := strconv.Atoi(ctx.args[0])
	if err != nil {
		return usagef("edit: invalid task number %q", ctx.args[0])
	}

	text := strings.Join(ctx.args[1:], " ")
	if hasPriorityPrefix(text) {
		return usagef("edit: replacement text begins with a priority; use pri to set priority")
	}
	if strings.TrimSpace(text) == "" {
		return usagef("edit: empty replacement text")
	}

	tasks, malformed, err := LoadForMutation(ctx.filePath)
	if err != nil {
		return err
	}
	ctx.noteMalformed(malformed)

	idx, err := Resolve(tasks, num)
	if err != nil {
		return err
	}
	t := tasks[idx]
	if t.Completed {
		return usagef("edit: task %d is completed", num)
	}

	t.Text = text
	tasks[idx] = t
	if err := Save(ctx.filePath, tasks); err != nil {
		return err
	}

	fmt.Fprintf(ctx.stdout, "%d %s\n", num, t.Render())
	return nil
}

// hasPriorityPrefix reports whether text begins with the grammar's exact
// uppercase priority prefix. Similar-looking lowercase, incomplete, or
// multi-letter parenthesized text remains ordinary replacement text.
func hasPriorityPrefix(text string) bool {
	return len(text) >= 4 && text[0] == '(' && text[1] >= 'A' && text[1] <= 'Z' &&
		text[2] == ')' && text[3] == ' '
}
