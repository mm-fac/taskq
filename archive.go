package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

// archive.go implements `taskq archive`: it moves ALL completed tasks out of
// the task file and into done.txt in the same directory (requirements.md,
// "Commands"). Open tasks and preserved malformed lines stay in the task file,
// in place; the completed tasks are appended to done.txt in file order. It
// prints `archived N task(s)` where N is the number moved.
//
// archive is the only two-file mutation, and its ordering is decided
// (requirements.md, "Atomicity"): (1) append the completed tasks to done.txt
// and flush, then (2) rewrite the task file without them. A crash between the
// two steps may duplicate tasks into both files but can never lose one; this is
// the accepted failure mode, documented in the `--help` text below.
//
// N=0 is a valid no-op that performs NO file writes at all: with no completed
// tasks, done.txt is not created and the task file is not rewritten. done.txt
// is a write-only append target here — archive never reads it back (that is out
// of scope in v0.1) — so step 1 is a plain O_APPEND write rather than the
// store's temp+rename rewrite, which would require reading the existing
// content. Step 2 reuses the store's atomic Save.

func init() {
	register(command{
		name:    "archive",
		summary: "move completed tasks to done.txt (archive)",
		run:     runArchive,
	})
}

// runArchive parses archive's (flag-only) arguments, loads the task file,
// partitions completed tasks from everything else, and — only when at least one
// completed task exists — appends them to done.txt and rewrites the task file
// without them, in that decided order. It prints `archived N task(s)`.
func runArchive(ctx *cmdContext) error {
	fs := flag.NewFlagSet("archive", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // we print our own taskq:-prefixed diagnostics
	fs.Usage = func() {}     // -h/--help is handled explicitly below
	if err := fs.Parse(ctx.args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printArchiveHelp(ctx.stdout)
			return nil
		}
		return usagef("archive: %v", err)
	}
	if rest := fs.Args(); len(rest) > 0 {
		return usagef("archive: unexpected argument %q", rest[0])
	}

	// archive addresses no task by number, so a missing task file is not an
	// error (there is simply nothing to archive): Load treats it as empty.
	tasks, malformed, err := Load(ctx.filePath)
	if err != nil {
		return err
	}
	ctx.noteMalformed(malformed)

	// Partition in file order: completed tasks move to done.txt; open tasks and
	// preserved malformed lines (which are never Completed) stay in the task
	// file, keeping their positions so the rewrite disturbs nothing else.
	completed := make([]Task, 0, len(tasks))
	remaining := make([]Task, 0, len(tasks))
	for _, t := range tasks {
		if t.Completed {
			completed = append(completed, t)
		} else {
			remaining = append(remaining, t)
		}
	}

	// N=0 no-op: no completed tasks means NO file writes at all — done.txt is
	// not created and the task file is not rewritten.
	if len(completed) == 0 {
		fmt.Fprintln(ctx.stdout, "archived 0 task(s)")
		return nil
	}

	// Decided two-file ordering: step 1 append+flush done.txt, step 2 rewrite
	// the task file. If step 2 fails after step 1, the completed tasks live in
	// both files (duplicated, never lost) — the accepted failure mode.
	if err := appendDone(ctx.donePath, completed); err != nil {
		return err
	}
	if err := Save(ctx.filePath, remaining); err != nil {
		return err
	}

	fmt.Fprintf(ctx.stdout, "archived %d task(s)\n", len(completed))
	return nil
}

// appendDone appends the given tasks, one rendered line each, to the done.txt
// at path, creating the file if absent, and flushes to disk before returning.
// Each line is written with exactly one trailing '\n'; since taskq only ever
// writes '\n'-terminated lines here, the file keeps exactly one trailing
// newline. It never reads existing content (done.txt is a write-only append
// target in v0.1), so this is an O_APPEND write rather than a temp+rename
// rewrite. Failures surface as *IOError (exit 2).
func appendDone(path string, tasks []Task) (err error) {
	f, oerr := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if oerr != nil {
		return &IOError{Op: "open", Path: path, Err: oerr}
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = &IOError{Op: "close", Path: path, Err: cerr}
		}
	}()

	var buf strings.Builder
	for _, t := range tasks {
		buf.WriteString(t.Render())
		buf.WriteByte('\n')
	}
	if _, err = f.WriteString(buf.String()); err != nil {
		return &IOError{Op: "append to", Path: path, Err: err}
	}
	// Flush to disk so step 1 is durable before step 2 rewrites the task file
	// (the decided ordering behind the accepted crash failure mode).
	if err = f.Sync(); err != nil {
		return &IOError{Op: "sync", Path: path, Err: err}
	}
	return nil
}

// printArchiveHelp writes archive's usage to w, documenting the decided
// two-step ordering and its accepted failure mode (requirements.md,
// "Atomicity").
func printArchiveHelp(w io.Writer) {
	fmt.Fprintln(w, "usage: taskq archive")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Move all completed tasks out of the task file into done.txt (in the")
	fmt.Fprintln(w, "same directory as the task file, created if absent), then rewrite the")
	fmt.Fprintln(w, "task file without them. Malformed lines are left in place. Prints")
	fmt.Fprintln(w, `"archived N task(s)". With no completed tasks it is a no-op that writes`)
	fmt.Fprintln(w, "nothing.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Ordering and accepted failure mode:")
	fmt.Fprintln(w, "  archive writes in two steps: (1) it appends the completed tasks to")
	fmt.Fprintln(w, "  done.txt and flushes, then (2) it rewrites the task file without them.")
	fmt.Fprintln(w, "  A crash between the two steps may duplicate tasks into both files, but")
	fmt.Fprintln(w, "  can never lose one.")
}
