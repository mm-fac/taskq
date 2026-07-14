package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// store.go is the task-file store: it loads a task file into an ordered list of
// classified lines (via ParseLine from task.go), rewrites it atomically, and
// resolves a 1-based task number to a target line with the error classes the
// CLI maps to exit codes. It implements the "Atomicity", "Trailing newline",
// "Malformed lines", and task-identity rules from requirements.md, plus the
// read/mutation missing-file distinction from "Exit codes and errors".

// --- typed errors -----------------------------------------------------------
//
// These are the distinct error types the CLI maps to process exit codes
// (mapping itself lives in the CLI item). Their exit class per
// requirements.md ("Exit codes and errors") is noted on each type.

// NoFileError reports that a number-addressing mutation (done/rm/pri/due) was
// attempted against a task file that does not exist. This is an I/O-class
// failure (exit 2): a command that addresses a task by number has nothing to
// address. Read commands treat a missing file as an empty listing instead
// (see Load), so this error is produced only by LoadForMutation.
type NoFileError struct{ Path string }

func (e *NoFileError) Error() string {
	return fmt.Sprintf("taskq: %s: no such file", e.Path)
}

// RangeError reports a task number outside 1..N, where N is the number of lines
// in the file (malformed lines included). It is a usage-class error (exit 1).
type RangeError struct {
	Num   int // the 1-based number the user gave
	Count int // total number of lines in the file
}

func (e *RangeError) Error() string {
	if e.Count == 0 {
		return fmt.Sprintf("taskq: task %d out of range (file has no lines)", e.Num)
	}
	return fmt.Sprintf("taskq: task %d out of range (1-%d)", e.Num, e.Count)
}

// MalformedTargetError reports that a task number resolved to a malformed line.
// Addressing a mutation at a malformed line is a usage error (exit 1): the line
// is preserved and never treated as a task.
type MalformedTargetError struct{ Num int }

func (e *MalformedTargetError) Error() string {
	return fmt.Sprintf("taskq: task %d is a malformed line", e.Num)
}

// IOError wraps a filesystem failure (read, write, temp-file, or rename) other
// than the missing-file case handled by NoFileError. It is an I/O-class error
// (exit 2) and unwraps to the underlying cause.
type IOError struct {
	Op   string // human-readable operation, e.g. "read" or "rename onto"
	Path string
	Err  error
}

func (e *IOError) Error() string {
	return fmt.Sprintf("taskq: %s %s: %v", e.Op, e.Path, e.Err)
}

func (e *IOError) Unwrap() error { return e.Err }

// --- load -------------------------------------------------------------------

// loadFile reads and parses the task file at path. existed reports whether the
// file was present (false only when it does not exist, which is never itself an
// error here); Load and LoadForMutation differ only in how they treat that.
func loadFile(path string) (tasks []Task, malformed int, existed bool, err error) {
	data, rerr := os.ReadFile(path)
	if rerr != nil {
		if os.IsNotExist(rerr) {
			return nil, 0, false, nil
		}
		return nil, 0, false, &IOError{Op: "read", Path: path, Err: rerr}
	}
	// A zero-byte file is an existing but empty task list: no lines.
	if len(data) == 0 {
		return nil, 0, true, nil
	}

	// Drop exactly one trailing newline (the file terminator this store always
	// writes). Any remaining blank lines are real, malformed lines that must be
	// preserved in place, so we must not trim them all.
	body := strings.TrimSuffix(string(data), "\n")
	lines := strings.Split(body, "\n")

	tasks = make([]Task, len(lines))
	for i, ln := range lines {
		t := ParseLine(ln)
		if t.Malformed {
			malformed++
		}
		tasks[i] = t
	}
	return tasks, malformed, true, nil
}

// Load reads the task file at path into an ordered slice of Tasks, one per line
// in file order, each classified via ParseLine. A missing file is not an error:
// it yields an empty slice, leaving callers to decide whether that is legal for
// their command (read commands list nothing; add creates the file). The second
// return is the count of malformed lines, so a command can emit the single
// "taskq: skipped N malformed line(s)" stderr note once.
func Load(path string) (tasks []Task, malformed int, err error) {
	tasks, malformed, _, err = loadFile(path)
	return tasks, malformed, err
}

// LoadForMutation is Load for commands that address a task by number
// (done/rm/pri/due). It differs in exactly one way: a missing task file is a
// NoFileError (I/O class, exit 2) rather than an empty list, because such a
// command has nothing to address.
func LoadForMutation(path string) (tasks []Task, malformed int, err error) {
	tasks, malformed, existed, err := loadFile(path)
	if err != nil {
		return nil, 0, err
	}
	if !existed {
		return nil, 0, &NoFileError{Path: path}
	}
	return tasks, malformed, nil
}

// --- atomic write -----------------------------------------------------------

// Save atomically rewrites the task file at path with tasks rendered one per
// line. The file always ends with exactly one '\n'; an empty task list writes a
// zero-byte file. Malformed tasks render byte-for-byte from their preserved Raw
// bytes, so a rewrite never disturbs a line it does not touch.
//
// The new content is written to a temp file in path's own directory, flushed,
// then renamed over path. rename(2) within one filesystem is atomic, so a
// reader never observes a partial file and a crash cannot truncate the
// original. On any failure the temp file is removed, leaving no litter behind.
func Save(path string, tasks []Task) (err error) {
	var buf strings.Builder
	for _, t := range tasks {
		buf.WriteString(t.Render())
		buf.WriteByte('\n')
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".taskq-*.tmp")
	if err != nil {
		return &IOError{Op: "create temp for", Path: path, Err: err}
	}
	tmpName := tmp.Name()
	// Remove the temp file on every error path. After a successful rename this
	// is a harmless no-op (the file has already moved to path).
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()

	// Match the destination's permissions when it already exists, else use a
	// conventional 0644 (os.CreateTemp creates the file mode 0600).
	mode := os.FileMode(0o644)
	if fi, statErr := os.Stat(path); statErr == nil {
		mode = fi.Mode().Perm()
	}
	if err = tmp.Chmod(mode); err != nil {
		return &IOError{Op: "chmod temp for", Path: path, Err: err}
	}

	if _, err = tmp.WriteString(buf.String()); err != nil {
		return &IOError{Op: "write", Path: path, Err: err}
	}
	if err = tmp.Sync(); err != nil {
		return &IOError{Op: "sync", Path: path, Err: err}
	}
	if err = tmp.Close(); err != nil {
		return &IOError{Op: "close temp for", Path: path, Err: err}
	}
	if err = os.Rename(tmpName, path); err != nil {
		return &IOError{Op: "rename onto", Path: path, Err: err}
	}
	return nil
}

// --- number resolution ------------------------------------------------------

// Resolve maps a 1-based task number to an index into tasks, validating that it
// addresses a real task. Identity is the line number over ALL lines, malformed
// included, so numbering matches a text editor (requirements.md, "The task
// file"). A number below 1 or above the line count is a RangeError; a number
// landing on a malformed line is a MalformedTargetError. Both are usage-class
// (exit 1); the caller maps them to messages and the process exit code.
func Resolve(tasks []Task, num int) (int, error) {
	if num < 1 || num > len(tasks) {
		return 0, &RangeError{Num: num, Count: len(tasks)}
	}
	idx := num - 1
	if tasks[idx].Malformed {
		return 0, &MalformedTargetError{Num: num}
	}
	return idx, nil
}
