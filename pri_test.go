package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// priFile returns a --file path inside a fresh temp dir so each pri test
// mutates its own isolated task file.
func priFile(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "todo.txt")
}

// runPriCase invokes the real dispatch (run) for `pri` with a pinned --today,
// returning the exit code and captured streams. Going through run exercises the
// registered command, global-flag parsing, and today injection as the binary
// would.
func runPriCase(t *testing.T, file string, args ...string) (int, string, string) {
	t.Helper()
	full := append([]string{"--today", "2026-07-15", "--file", file, "pri"}, args...)
	var out, errb bytes.Buffer
	code := run(full, &out, &errb, noEnv)
	return code, out.String(), errb.String()
}

// TestPriHappyPath covers setting, replacing, uppercase-coercing, and clearing
// a priority: the addressed line is rewritten with (or without) its priority
// while creation date and text are untouched, the printed line carries its
// number, and the file ends with exactly one trailing newline.
func TestPriHappyPath(t *testing.T) {
	cases := []struct {
		name     string
		seed     string
		args     []string
		wantOut  string
		wantFile string
	}{
		{
			name:     "set on unprioritized task",
			seed:     "2026-07-14 call the bank +errands\n",
			args:     []string{"1", "A"},
			wantOut:  "1 (A) 2026-07-14 call the bank +errands\n",
			wantFile: "(A) 2026-07-14 call the bank +errands\n",
		},
		{
			name:     "replace existing priority",
			seed:     "(A) 2026-07-14 call the bank\n",
			args:     []string{"1", "C"},
			wantOut:  "1 (C) 2026-07-14 call the bank\n",
			wantFile: "(C) 2026-07-14 call the bank\n",
		},
		{
			name:     "lowercase coerced to uppercase",
			seed:     "2026-07-14 buy milk\n",
			args:     []string{"1", "b"},
			wantOut:  "1 (B) 2026-07-14 buy milk\n",
			wantFile: "(B) 2026-07-14 buy milk\n",
		},
		{
			name:     "none removes priority",
			seed:     "(A) 2026-07-14 call the bank\n",
			args:     []string{"1", "none"},
			wantOut:  "1 2026-07-14 call the bank\n",
			wantFile: "2026-07-14 call the bank\n",
		},
		{
			name:     "none on already-unprioritized task is a no-op set",
			seed:     "2026-07-14 buy milk\n",
			args:     []string{"1", "none"},
			wantOut:  "1 2026-07-14 buy milk\n",
			wantFile: "2026-07-14 buy milk\n",
		},
		{
			name:     "uppercase NONE also removes priority",
			seed:     "(D) 2026-07-14 call the bank\n",
			args:     []string{"1", "NONE"},
			wantOut:  "1 2026-07-14 call the bank\n",
			wantFile: "2026-07-14 call the bank\n",
		},
		{
			name:     "set on task without creation date",
			seed:     "water plants\n",
			args:     []string{"1", "z"},
			wantOut:  "1 (Z) water plants\n",
			wantFile: "(Z) water plants\n",
		},
		{
			name:     "second of two tasks",
			seed:     "2026-07-14 first\n2026-07-14 second\n",
			args:     []string{"2", "A"},
			wantOut:  "2 (A) 2026-07-14 second\n",
			wantFile: "2026-07-14 first\n(A) 2026-07-14 second\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			file := priFile(t)
			seedFile(t, file, c.seed)
			code, out, errb := runPriCase(t, file, c.args...)
			if code != 0 {
				t.Fatalf("exit = %d, want 0 (stderr %q)", code, errb)
			}
			if out != c.wantOut {
				t.Errorf("stdout = %q, want %q", out, c.wantOut)
			}
			if errb != "" {
				t.Errorf("stderr = %q, want empty", errb)
			}
			if got := readFile(t, file); got != c.wantFile {
				t.Errorf("file = %q, want %q", got, c.wantFile)
			}
		})
	}
}

// TestPriCompletedTaskRejected covers the completed-task rejection: setting or
// clearing a priority on a completed task is a usage error (exit 1) that writes
// nothing and leaves the file unchanged.
func TestPriCompletedTaskRejected(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"set letter", []string{"1", "A"}},
		{"clear none", []string{"1", "none"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			file := priFile(t)
			seed := "x 2026-07-10 2026-07-14 call the bank +errands\n"
			seedFile(t, file, seed)
			code, out, errb := runPriCase(t, file, c.args...)
			if code != 1 {
				t.Errorf("exit = %d, want 1 (stderr %q)", code, errb)
			}
			if out != "" {
				t.Errorf("stdout = %q, want empty", out)
			}
			if !bytes.HasPrefix([]byte(errb), []byte("taskq: ")) {
				t.Errorf("stderr = %q, want taskq: prefix", errb)
			}
			if got := readFile(t, file); got != seed {
				t.Errorf("file = %q, want unchanged %q", got, seed)
			}
		})
	}
}

// TestPriUsageErrors covers the usage-class failures (exit 1): a bad priority
// argument, a number out of range, a number landing on a malformed line, a
// non-numeric number, and the wrong argument count. Each writes nothing to
// stdout, emits a taskq:-prefixed diagnostic, and leaves the file unchanged.
func TestPriUsageErrors(t *testing.T) {
	cases := []struct {
		name string
		seed string
		args []string
	}{
		{"priority two letters", "2026-07-14 real\n", []string{"1", "AB"}},
		{"priority digit", "2026-07-14 real\n", []string{"1", "1"}},
		{"priority empty", "2026-07-14 real\n", []string{"1", ""}},
		{"priority word", "2026-07-14 real\n", []string{"1", "high"}},
		{"out of range high", "2026-07-14 only task\n", []string{"2", "A"}},
		{"out of range zero", "2026-07-14 only task\n", []string{"0", "A"}},
		{"negative", "2026-07-14 only task\n", []string{"-1", "A"}},
		{"malformed target", "\n2026-07-14 real\n", []string{"1", "A"}},
		// CAP-15: a line that looks like a completed task but carries a
		// calendar-invalid completion date is malformed; addressing pri at it is
		// a usage error and the file is left byte-for-byte unchanged.
		{"broken-completion target", "x 2026-13-99 broken\n", []string{"1", "A"}},
		{"non-numeric number", "2026-07-14 real\n", []string{"abc", "A"}},
		{"no args", "2026-07-14 real\n", nil},
		{"one arg", "2026-07-14 real\n", []string{"1"}},
		{"too many args", "2026-07-14 real\n", []string{"1", "A", "B"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			file := priFile(t)
			seedFile(t, file, c.seed)
			code, out, errb := runPriCase(t, file, c.args...)
			if code != 1 {
				t.Errorf("exit = %d, want 1 (stderr %q)", code, errb)
			}
			if out != "" {
				t.Errorf("stdout = %q, want empty", out)
			}
			if !bytes.HasPrefix([]byte(errb), []byte("taskq: ")) {
				t.Errorf("stderr = %q, want taskq: prefix", errb)
			}
			if got := readFile(t, file); got != c.seed {
				t.Errorf("file = %q, want unchanged %q", got, c.seed)
			}
		})
	}
}

// TestPriMissingFileIsIOError asserts that addressing a task by number against
// a missing task file is an I/O error (exit 2), not a usage error, and creates
// no file — even though the priority argument itself is well-formed.
func TestPriMissingFileIsIOError(t *testing.T) {
	file := priFile(t) // never created
	code, out, errb := runPriCase(t, file, "1", "A")
	if code != 2 {
		t.Errorf("exit = %d, want 2 (stderr %q)", code, errb)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if !bytes.HasPrefix([]byte(errb), []byte("taskq: ")) {
		t.Errorf("stderr = %q, want taskq: prefix", errb)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("file %q exists after pri on missing file, want nothing written", file)
	}
}

// TestPriPreservesMalformedAndTrailingNewline sets a priority in a file that
// also holds malformed lines: the malformed lines survive byte-for-byte in
// place, the target is reprioritized, the file ends in exactly one newline, and
// the once-per-command malformed note is emitted on stderr alongside the
// resulting line's stdout.
func TestPriPreservesMalformedAndTrailingNewline(t *testing.T) {
	file := priFile(t)
	// A blank line and a spaces-only line (both malformed) around two tasks; no
	// trailing newline on the seed to prove Save normalises to exactly one.
	seed := "\n(A) 2026-07-14 call the bank\n   \n2026-07-14 buy milk"
	seedFile(t, file, seed)

	// Line 4 is the second task (lines 1 and 3 are malformed).
	code, out, errb := runPriCase(t, file, "4", "b")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr %q)", code, errb)
	}
	if want := "4 (B) 2026-07-14 buy milk\n"; out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
	if want := "taskq: skipped 2 malformed line(s)\n"; errb != want {
		t.Errorf("stderr = %q, want %q", errb, want)
	}
	want := "\n(A) 2026-07-14 call the bank\n   \n(B) 2026-07-14 buy milk\n"
	if got := readFile(t, file); got != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}

// TestPriAtomicNoTempLitter asserts the atomic write leaves no temp file behind
// in the task file's directory after a successful reprioritization.
func TestPriAtomicNoTempLitter(t *testing.T) {
	file := priFile(t)
	seedFile(t, file, "2026-07-14 buy milk\n")
	if code, _, errb := runPriCase(t, file, "1", "A"); code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr %q)", code, errb)
	}
	entries, err := os.ReadDir(filepath.Dir(file))
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "todo.txt" {
			t.Errorf("unexpected leftover file %q in task dir", e.Name())
		}
	}
}
