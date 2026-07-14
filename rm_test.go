package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// rmFile returns a --file path inside a fresh temp dir so each rm test mutates
// its own isolated task file.
func rmFile(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "todo.txt")
}

// runRmCase invokes the real dispatch (run) for `rm` with a pinned --today,
// returning the exit code and captured streams. Going through run exercises the
// registered command, global-flag parsing, and today injection as the binary
// would. (rm never writes a date, but --today is pinned for hermeticity.)
func runRmCase(t *testing.T, file string, args ...string) (int, string, string) {
	t.Helper()
	full := append([]string{"--today", "2026-07-15", "--file", file, "rm"}, args...)
	var out, errb bytes.Buffer
	code := run(full, &out, &errb, noEnv)
	return code, out.String(), errb.String()
}

// TestRmHappyPath covers deleting a task — open or completed — from a file: the
// deleted line is printed prefixed `removed: `, the file is rewritten without
// it, remaining lines keep their order, and the file ends in exactly one
// trailing newline.
func TestRmHappyPath(t *testing.T) {
	cases := []struct {
		name     string
		seed     string
		num      string
		wantOut  string
		wantFile string
	}{
		{
			name:     "open task, full grammar",
			seed:     "(A) 2026-07-14 call the bank +errands @phone due:2026-07-20\n",
			num:      "1",
			wantOut:  "removed: (A) 2026-07-14 call the bank +errands @phone due:2026-07-20\n",
			wantFile: "",
		},
		{
			name:     "completed task",
			seed:     "x 2026-07-15 2026-07-14 call the bank +errands\n",
			num:      "1",
			wantOut:  "removed: x 2026-07-15 2026-07-14 call the bank +errands\n",
			wantFile: "",
		},
		{
			name:     "middle of three, others keep order",
			seed:     "2026-07-14 first\n(C) 2026-07-14 second\n2026-07-14 third\n",
			num:      "2",
			wantOut:  "removed: (C) 2026-07-14 second\n",
			wantFile: "2026-07-14 first\n2026-07-14 third\n",
		},
		{
			name:     "last of two",
			seed:     "2026-07-14 first\nx 2026-07-13 2026-07-14 second\n",
			num:      "2",
			wantOut:  "removed: x 2026-07-13 2026-07-14 second\n",
			wantFile: "2026-07-14 first\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			file := rmFile(t)
			seedFile(t, file, c.seed)
			code, out, errb := runRmCase(t, file, c.num)
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

// TestRmOnlyTaskYieldsZeroByteFile asserts that deleting the sole task leaves a
// zero-byte file (not a lone newline).
func TestRmOnlyTaskYieldsZeroByteFile(t *testing.T) {
	file := rmFile(t)
	seedFile(t, file, "2026-07-14 buy milk\n")
	code, out, errb := runRmCase(t, file, "1")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr %q)", code, errb)
	}
	if want := "removed: 2026-07-14 buy milk\n"; out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
	if got := readFile(t, file); got != "" {
		t.Errorf("file = %q, want zero bytes", got)
	}
	fi, err := os.Stat(file)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if fi.Size() != 0 {
		t.Errorf("file size = %d, want 0", fi.Size())
	}
}

// TestRmPreservesMalformedAndTrailingNewline deletes a task in a file that also
// holds malformed lines: the malformed lines survive byte-for-byte in place,
// the target line is gone, the file ends in exactly one newline, and the
// once-per-command malformed note is emitted on stderr.
func TestRmPreservesMalformedAndTrailingNewline(t *testing.T) {
	file := rmFile(t)
	// A blank line and a spaces-only line (both malformed) around two tasks; no
	// trailing newline on the seed to prove Save normalises to exactly one.
	seed := "\n(A) 2026-07-14 call the bank\n   \n2026-07-14 buy milk"
	seedFile(t, file, seed)

	// Line 2 is the prioritized task (lines 1 and 3 are malformed).
	code, out, errb := runRmCase(t, file, "2")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr %q)", code, errb)
	}
	if want := "removed: (A) 2026-07-14 call the bank\n"; out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
	if want := "taskq: skipped 2 malformed line(s)\n"; errb != want {
		t.Errorf("stderr = %q, want %q", errb, want)
	}
	// The two malformed lines stay in place; the target is gone; one trailing \n.
	want := "\n   \n2026-07-14 buy milk\n"
	if got := readFile(t, file); got != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}

// TestRmUsageErrors covers the usage-class failures (exit 1): a number out of
// range, a number landing on a malformed line, a non-numeric argument, and the
// wrong argument count. Each writes nothing to stdout and leaves the file
// unchanged.
func TestRmUsageErrors(t *testing.T) {
	cases := []struct {
		name string
		seed string
		args []string
	}{
		{"out of range high", "2026-07-14 only task\n", []string{"2"}},
		{"out of range zero", "2026-07-14 only task\n", []string{"0"}},
		{"negative", "2026-07-14 only task\n", []string{"-1"}},
		{"malformed target", "\n2026-07-14 real\n", []string{"1"}},
		{"non-numeric", "2026-07-14 real\n", []string{"abc"}},
		{"no args", "2026-07-14 real\n", nil},
		{"too many args", "2026-07-14 real\n", []string{"1", "2"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			file := rmFile(t)
			seedFile(t, file, c.seed)
			code, out, errb := runRmCase(t, file, c.args...)
			if code != 1 {
				t.Errorf("exit = %d, want 1 (stderr %q)", code, errb)
			}
			if out != "" {
				t.Errorf("stdout = %q, want empty", out)
			}
			if !bytes.HasPrefix([]byte(errb), []byte("taskq: ")) {
				t.Errorf("stderr = %q, want taskq: prefix", errb)
			}
			// A failed rm must not rewrite the file.
			if got := readFile(t, file); got != c.seed {
				t.Errorf("file = %q, want unchanged %q", got, c.seed)
			}
		})
	}
}

// TestRmMissingFileIsIOError asserts that addressing a task by number against a
// missing task file is an I/O error (exit 2), not a usage error, and creates no
// file.
func TestRmMissingFileIsIOError(t *testing.T) {
	file := rmFile(t) // never created
	code, out, errb := runRmCase(t, file, "1")
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
		t.Errorf("file %q exists after rm on missing file, want nothing written", file)
	}
}

// TestRmAtomicNoTempLitter asserts the atomic write leaves no temp file behind
// in the task file's directory after a successful deletion.
func TestRmAtomicNoTempLitter(t *testing.T) {
	file := rmFile(t)
	seedFile(t, file, "2026-07-14 buy milk\n2026-07-14 walk dog\n")
	if code, _, errb := runRmCase(t, file, "1"); code != 0 {
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
