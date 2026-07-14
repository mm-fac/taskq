package main

import (
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"
)

// list.go implements `taskq list`: it prints tasks from the task file, one per
// line, each prefixed by `N ` where N is the task's 1-based line number in the
// current file (no padding). It is read-only — it uses the store's Load and
// never touches done.txt or mutates anything.
//
// Line numbers are the file line-number identity (requirements.md, "The task
// file"): every line consumes a number, malformed lines included, so numbering
// matches a text editor; malformed lines are skipped from the listing but still
// advance the count and are reported once via the shared malformed note.
//
// Scope selects which non-malformed lines are eligible: the default is OPEN
// tasks from the task file only; `--all` adds completed tasks; `--done` shows
// completed tasks only. `--all` and `--done` conflict (usage error). None of
// these read done.txt. The `--project`, `--context`, and `--overdue` filters
// then AND together over the eligible tasks. Finally an optional `--sort`
// reorders the survivors stably (ties and no-value groups keep file order),
// with the line number staying attached to its task through the reordering.

func init() {
	register(command{
		name:    "list",
		summary: "print tasks (list [--project P] [--context C] [--overdue] [--sort pri|due|created] [--all|--done])",
		run:     runList,
	})
}

// listEntry pairs a displayable task with its 1-based file line number. The
// number is captured before any filtering or sorting so it stays the file
// line-number identity rather than a re-count of display position.
type listEntry struct {
	num  int
	task Task
}

// runList parses the list flags from ctx.args, loads the task file (a missing
// file is an empty listing, not an error), applies the scope, filters, and sort,
// and prints the surviving tasks with their file line numbers.
func runList(ctx *cmdContext) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // we print our own taskq:-prefixed diagnostics
	var project, context, sortKey string
	var overdue, all, done bool
	fs.StringVar(&project, "project", "", "keep tasks with the exact token +P")
	fs.StringVar(&context, "context", "", "keep tasks with the exact token @C")
	fs.BoolVar(&overdue, "overdue", false, "keep tasks whose due: date is before today")
	fs.StringVar(&sortKey, "sort", "", "sort by pri, due, or created")
	fs.BoolVar(&all, "all", false, "show open and completed tasks")
	fs.BoolVar(&done, "done", false, "show completed tasks only")
	if err := fs.Parse(ctx.args); err != nil {
		return usagef("list: %v", err)
	}
	if rest := fs.Args(); len(rest) > 0 {
		return usagef("list: unexpected argument %q", rest[0])
	}

	// --all and --done select mutually exclusive scopes.
	if all && done {
		return usagef("list: --all and --done cannot be combined")
	}
	switch sortKey {
	case "", "pri", "due", "created":
	default:
		return usagef("list: invalid --sort %q: want pri, due, or created", sortKey)
	}

	// Scope: open tasks are shown unless --done narrows to completed only;
	// completed tasks are shown only with --all or --done. None reads done.txt.
	showOpen := !done
	showDone := all || done

	tasks, malformed, err := Load(ctx.filePath)
	if err != nil {
		return err
	}
	ctx.noteMalformed(malformed)

	entries := make([]listEntry, 0, len(tasks))
	for i, t := range tasks {
		if t.Malformed {
			continue // skipped from the listing, but still consumed line i+1
		}
		if t.Completed {
			if !showDone {
				continue
			}
		} else if !showOpen {
			continue
		}
		if project != "" && !hasToken(t.Text, "+"+project) {
			continue
		}
		if context != "" && !hasToken(t.Text, "@"+context) {
			continue
		}
		if overdue {
			d := dueDate(t)
			if d == "" || d >= ctx.todayStr {
				continue // no due: date, or not strictly before today
			}
		}
		entries = append(entries, listEntry{num: i + 1, task: t})
	}

	// Stable sort: ties and no-value groups keep file order (SliceStable), and
	// the line number rides along on each entry so it stays attached to its task.
	switch sortKey {
	case "pri":
		sort.SliceStable(entries, func(i, j int) bool {
			return priorityRank(entries[i].task) < priorityRank(entries[j].task)
		})
	case "due":
		sort.SliceStable(entries, func(i, j int) bool {
			return lessDateNoValueLast(dueDate(entries[i].task), dueDate(entries[j].task))
		})
	case "created":
		sort.SliceStable(entries, func(i, j int) bool {
			return lessDateNoValueLast(entries[i].task.CreationDate, entries[j].task.CreationDate)
		})
	}

	for _, e := range entries {
		fmt.Fprintf(ctx.stdout, "%d %s\n", e.num, e.task.Render())
	}
	return nil
}

// hasToken reports whether text contains tok as a whole whitespace-delimited
// token (exact, case-sensitive match). This keeps `+P` from matching `+Project`
// and matches the requirement's "exact token `+P`/`@C`" semantics.
func hasToken(text, tok string) bool {
	for _, f := range strings.Fields(text) {
		if f == tok {
			return true
		}
	}
	return false
}

// dueDate returns the YYYY-MM-DD of the task's first valid `due:` token, or ""
// when the task has none. Because dates are zero-padded and fixed-width, the
// returned string compares chronologically under ordinary string ordering.
func dueDate(t Task) string {
	for _, f := range strings.Fields(t.Text) {
		if d, ok := strings.CutPrefix(f, "due:"); ok && validDate(d) {
			return d
		}
	}
	return ""
}

// priorityRank maps a task to its --sort pri key: 'A'-'Z' order first, then all
// unprioritized tasks (rank above 'Z') after. Equal ranks compare equal, so a
// stable sort preserves file order within a priority and within the
// unprioritized group.
func priorityRank(t Task) int {
	if t.Priority == 0 {
		return int('Z') + 1
	}
	return int(t.Priority)
}

// lessDateNoValueLast orders two date strings earliest-first with the empty
// (no-value) string sorting after every real date. Two empty strings compare
// equal, so a stable sort keeps file order within the no-value group.
func lessDateNoValueLast(a, b string) bool {
	if (a == "") != (b == "") {
		return a != "" // the one with a date comes first
	}
	return a < b // both dated: chronological; both empty: equal
}
