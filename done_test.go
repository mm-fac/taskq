package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// doneFile returns a --file path inside a fresh temp dir so each done test
// mutates its own isolated task file.
func doneFile(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "todo.txt")
}

// seedFile writes content to path verbatim, so a test can pin the exact bytes
// (including a missing trailing newline) the store then loads.
func seedFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("seed write %q: %v", path, err)
	}
}

// runDoneCase invokes the real dispatch (run) for `done` with a pinned --today,
// returning the exit code and captured streams. Going through run exercises the
// registered command, global-flag parsing, and today injection as the binary
// would.
func runDoneCase(t *testing.T, file string, args ...string) (int, string, string) {
	t.Helper()
	full := append([]string{"--today", "2026-07-15", "--file", file, "done"}, args...)
	var out, errb bytes.Buffer
	code := run(full, &out, &errb, noEnv)
	return code, out.String(), errb.String()
}

// TestDoneHappyPath covers the happy path including the priority drop: an open,
// prioritized task is completed by prefixing `x <today> `, its priority is
// dropped, the creation date is kept, the printed line carries its number, and
// the file is rewritten with exactly one trailing newline.
func TestDoneHappyPath(t *testing.T) {
	cases := []struct {
		name     string
		seed     string
		num      string
		wantOut  string
		wantFile string
	}{
		{
			name:     "priority dropped",
			seed:     "(A) 2026-07-14 call the bank +errands @phone due:2026-07-20\n",
			num:      "1",
			wantOut:  "1 x 2026-07-15 2026-07-14 call the bank +errands @phone due:2026-07-20\n",
			wantFile: "x 2026-07-15 2026-07-14 call the bank +errands @phone due:2026-07-20\n",
		},
		{
			name:     "no priority to drop",
			seed:     "2026-07-14 buy milk\n",
			num:      "1",
			wantOut:  "1 x 2026-07-15 2026-07-14 buy milk\n",
			wantFile: "x 2026-07-15 2026-07-14 buy milk\n",
		},
		{
			name:     "no creation date",
			seed:     "(B) water plants\n",
			num:      "1",
			wantOut:  "1 x 2026-07-15 water plants\n",
			wantFile: "x 2026-07-15 water plants\n",
		},
		{
			name:     "second of two open tasks",
			seed:     "2026-07-14 first\n(C) 2026-07-14 second\n",
			num:      "2",
			wantOut:  "2 x 2026-07-15 2026-07-14 second\n",
			wantFile: "2026-07-14 first\nx 2026-07-15 2026-07-14 second\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			file := doneFile(t)
			seedFile(t, file, c.seed)
			code, out, errb := runDoneCase(t, file, c.num)
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

// TestDoneIdempotentNoOp covers the decided idempotent no-op: running done on
// an already-completed task leaves the line byte-for-byte unchanged, prints it
// unchanged prefixed by its number, exits 0, and emits the stderr note.
func TestDoneIdempotentNoOp(t *testing.T) {
	file := doneFile(t)
	seed := "x 2026-07-10 2026-07-14 call the bank +errands\n"
	seedFile(t, file, seed)

	code, out, errb := runDoneCase(t, file, "1")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr %q)", code, errb)
	}
	if want := "1 x 2026-07-10 2026-07-14 call the bank +errands\n"; out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
	if want := "taskq: task 1 already done\n"; errb != want {
		t.Errorf("stderr = %q, want %q", errb, want)
	}
	// The file is left exactly as seeded — no rewrite, no changed date.
	if got := readFile(t, file); got != seed {
		t.Errorf("file = %q, want unchanged %q", got, seed)
	}
}

// TestDoneUsageErrors covers the usage-class failures (exit 1): a number out of
// range, a number landing on a malformed line, a non-numeric argument, and the
// wrong argument count. Each writes nothing to stdout and emits a taskq:-
// prefixed diagnostic.
func TestDoneUsageErrors(t *testing.T) {
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
			file := doneFile(t)
			seedFile(t, file, c.seed)
			code, out, errb := runDoneCase(t, file, c.args...)
			if code != 1 {
				t.Errorf("exit = %d, want 1 (stderr %q)", code, errb)
			}
			if out != "" {
				t.Errorf("stdout = %q, want empty", out)
			}
			if !bytes.HasPrefix([]byte(errb), []byte("taskq: ")) {
				t.Errorf("stderr = %q, want taskq: prefix", errb)
			}
			// A failed done must not rewrite the file.
			if got := readFile(t, file); got != c.seed {
				t.Errorf("file = %q, want unchanged %q", got, c.seed)
			}
		})
	}
}

// TestDoneMissingFileIsIOError asserts that addressing a task by number against
// a missing task file is an I/O error (exit 2), not a usage error, and creates
// no file.
func TestDoneMissingFileIsIOError(t *testing.T) {
	file := doneFile(t) // never created
	code, out, errb := runDoneCase(t, file, "1")
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
		t.Errorf("file %q exists after done on missing file, want nothing written", file)
	}
}

// TestDonePreservesMalformedAndTrailingNewline completes a task in a file that
// also holds malformed lines: the malformed lines survive byte-for-byte in
// place, the target is completed, the file ends in exactly one newline, and the
// once-per-command malformed note is emitted on stderr alongside the completed
// line's stdout.
func TestDonePreservesMalformedAndTrailingNewline(t *testing.T) {
	file := doneFile(t)
	// A blank line and a spaces-only line (both malformed) around two tasks; no
	// trailing newline on the seed to prove Save normalises to exactly one.
	seed := "\n(A) 2026-07-14 call the bank\n   \n2026-07-14 buy milk"
	seedFile(t, file, seed)

	// Line 2 is the prioritized task (lines 1 and 3 are malformed).
	code, out, errb := runDoneCase(t, file, "2")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr %q)", code, errb)
	}
	if want := "2 x 2026-07-15 2026-07-14 call the bank\n"; out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
	if want := "taskq: skipped 2 malformed line(s)\n"; errb != want {
		t.Errorf("stderr = %q, want %q", errb, want)
	}
	want := "\nx 2026-07-15 2026-07-14 call the bank\n   \n2026-07-14 buy milk\n"
	if got := readFile(t, file); got != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}

// TestDoneAtomicNoTempLitter asserts the atomic write leaves no temp file
// behind in the task file's directory after a successful completion.
func TestDoneAtomicNoTempLitter(t *testing.T) {
	file := doneFile(t)
	seedFile(t, file, "2026-07-14 buy milk\n")
	if code, _, errb := runDoneCase(t, file, "1"); code != 0 {
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
