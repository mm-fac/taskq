package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// dueFile returns a --file path inside a fresh temp dir so each due test
// mutates its own isolated task file.
func dueFile(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "todo.txt")
}

// runDueCase invokes the real dispatch (run) for `due` with a pinned --today,
// returning the exit code and captured streams. Going through run exercises the
// registered command, global-flag parsing, and today injection as the binary
// would.
func runDueCase(t *testing.T, file string, args ...string) (int, string, string) {
	t.Helper()
	full := append([]string{"--today", "2026-07-15", "--file", file, "due"}, args...)
	var out, errb bytes.Buffer
	code := run(full, &out, &errb, noEnv)
	return code, out.String(), errb.String()
}

// TestDueHappyPath covers appending, in-place replacement, the multiple-due:
// reconciliation, and removal via `none`: the addressed line is rewritten with
// (or without) its due: token while completion state, priority, creation date,
// and non-due text are untouched, the printed line carries its number, and the
// file ends with exactly one trailing newline.
func TestDueHappyPath(t *testing.T) {
	cases := []struct {
		name     string
		seed     string
		args     []string
		wantOut  string
		wantFile string
	}{
		{
			name:     "append when no due token",
			seed:     "2026-07-14 call the bank +errands\n",
			args:     []string{"1", "2026-07-20"},
			wantOut:  "1 2026-07-14 call the bank +errands due:2026-07-20\n",
			wantFile: "2026-07-14 call the bank +errands due:2026-07-20\n",
		},
		{
			name:     "replace existing due token in place",
			seed:     "2026-07-14 call the bank due:2026-07-20 @phone\n",
			args:     []string{"1", "2026-08-01"},
			wantOut:  "1 2026-07-14 call the bank due:2026-08-01 @phone\n",
			wantFile: "2026-07-14 call the bank due:2026-08-01 @phone\n",
		},
		{
			name:     "replace first of multiple due tokens, drop the rest",
			seed:     "2026-07-14 pay rent due:2026-07-20 soon due:2026-07-25 really\n",
			args:     []string{"1", "2026-09-09"},
			wantOut:  "1 2026-07-14 pay rent due:2026-09-09 soon really\n",
			wantFile: "2026-07-14 pay rent due:2026-09-09 soon really\n",
		},
		{
			name:     "none removes the due token",
			seed:     "2026-07-14 call the bank due:2026-07-20 @phone\n",
			args:     []string{"1", "none"},
			wantOut:  "1 2026-07-14 call the bank @phone\n",
			wantFile: "2026-07-14 call the bank @phone\n",
		},
		{
			name:     "none removes first and remaining due tokens",
			seed:     "2026-07-14 pay rent due:2026-07-20 soon due:2026-07-25\n",
			args:     []string{"1", "none"},
			wantOut:  "1 2026-07-14 pay rent soon\n",
			wantFile: "2026-07-14 pay rent soon\n",
		},
		{
			name:     "none on task without a due token is a no-op set",
			seed:     "2026-07-14 buy milk\n",
			args:     []string{"1", "none"},
			wantOut:  "1 2026-07-14 buy milk\n",
			wantFile: "2026-07-14 buy milk\n",
		},
		{
			name:     "uppercase NONE also removes the due token",
			seed:     "2026-07-14 call the bank due:2026-07-20\n",
			args:     []string{"1", "NONE"},
			wantOut:  "1 2026-07-14 call the bank\n",
			wantFile: "2026-07-14 call the bank\n",
		},
		{
			name:     "append preserves priority and creation date",
			seed:     "(A) 2026-07-14 water plants +home\n",
			args:     []string{"1", "2026-07-31"},
			wantOut:  "1 (A) 2026-07-14 water plants +home due:2026-07-31\n",
			wantFile: "(A) 2026-07-14 water plants +home due:2026-07-31\n",
		},
		{
			name:     "append on task without a creation date",
			seed:     "water plants\n",
			args:     []string{"1", "2026-07-31"},
			wantOut:  "1 water plants due:2026-07-31\n",
			wantFile: "water plants due:2026-07-31\n",
		},
		{
			name:     "second of two tasks",
			seed:     "2026-07-14 first\n2026-07-14 second\n",
			args:     []string{"2", "2026-07-20"},
			wantOut:  "2 2026-07-14 second due:2026-07-20\n",
			wantFile: "2026-07-14 first\n2026-07-14 second due:2026-07-20\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			file := dueFile(t)
			seedFile(t, file, c.seed)
			code, out, errb := runDueCase(t, file, c.args...)
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

// TestDueCompletedTaskRejected covers the completed-task rejection: setting or
// clearing a due date on a completed task is a usage error (exit 1) that writes
// nothing and leaves the file unchanged.
func TestDueCompletedTaskRejected(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"set date", []string{"1", "2026-07-20"}},
		{"clear none", []string{"1", "none"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			file := dueFile(t)
			seed := "x 2026-07-10 2026-07-14 call the bank due:2026-07-20\n"
			seedFile(t, file, seed)
			code, out, errb := runDueCase(t, file, c.args...)
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

// TestDueUsageErrors covers the usage-class failures (exit 1): an invalid date
// argument (non-calendar-valid, non-zero-padded, or empty), a number out of
// range, a number landing on a malformed line, a non-numeric number, and the
// wrong argument count. Each writes nothing to stdout, emits a taskq:-prefixed
// diagnostic, and leaves the file unchanged.
func TestDueUsageErrors(t *testing.T) {
	cases := []struct {
		name string
		seed string
		args []string
	}{
		{"date not calendar valid", "2026-07-14 real\n", []string{"1", "2026-02-30"}},
		{"date month out of range", "2026-07-14 real\n", []string{"1", "2026-13-01"}},
		{"date not zero padded", "2026-07-14 real\n", []string{"1", "2026-7-14"}},
		{"date wrong shape", "2026-07-14 real\n", []string{"1", "07/20/2026"}},
		{"date empty", "2026-07-14 real\n", []string{"1", ""}},
		{"date word", "2026-07-14 real\n", []string{"1", "tomorrow"}},
		{"out of range high", "2026-07-14 only task\n", []string{"2", "2026-07-20"}},
		{"out of range zero", "2026-07-14 only task\n", []string{"0", "2026-07-20"}},
		{"negative", "2026-07-14 only task\n", []string{"-1", "2026-07-20"}},
		{"malformed target", "\n2026-07-14 real\n", []string{"1", "2026-07-20"}},
		{"non-numeric number", "2026-07-14 real\n", []string{"abc", "2026-07-20"}},
		{"no args", "2026-07-14 real\n", nil},
		{"one arg", "2026-07-14 real\n", []string{"1"}},
		{"too many args", "2026-07-14 real\n", []string{"1", "2026-07-20", "extra"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			file := dueFile(t)
			seedFile(t, file, c.seed)
			code, out, errb := runDueCase(t, file, c.args...)
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

// TestDueMissingFileIsIOError asserts that addressing a task by number against a
// missing task file is an I/O error (exit 2), not a usage error, and creates no
// file — even though the date argument itself is well-formed.
func TestDueMissingFileIsIOError(t *testing.T) {
	file := dueFile(t) // never created
	code, out, errb := runDueCase(t, file, "1", "2026-07-20")
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
		t.Errorf("file %q exists after due on missing file, want nothing written", file)
	}
}

// TestDuePreservesMalformedAndTrailingNewline sets a due date in a file that
// also holds malformed lines: the malformed lines survive byte-for-byte in
// place, the target gains its due: token, the file ends in exactly one newline,
// and the once-per-command malformed note is emitted on stderr alongside the
// resulting line's stdout.
func TestDuePreservesMalformedAndTrailingNewline(t *testing.T) {
	file := dueFile(t)
	// A blank line and a spaces-only line (both malformed) around two tasks; no
	// trailing newline on the seed to prove Save normalises to exactly one.
	seed := "\n(A) 2026-07-14 call the bank\n   \n2026-07-14 buy milk"
	seedFile(t, file, seed)

	// Line 4 is the second task (lines 1 and 3 are malformed).
	code, out, errb := runDueCase(t, file, "4", "2026-07-20")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr %q)", code, errb)
	}
	if want := "4 2026-07-14 buy milk due:2026-07-20\n"; out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
	if want := "taskq: skipped 2 malformed line(s)\n"; errb != want {
		t.Errorf("stderr = %q, want %q", errb, want)
	}
	want := "\n(A) 2026-07-14 call the bank\n   \n2026-07-14 buy milk due:2026-07-20\n"
	if got := readFile(t, file); got != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}

// TestDueAtomicNoTempLitter asserts the atomic write leaves no temp file behind
// in the task file's directory after a successful due-date set.
func TestDueAtomicNoTempLitter(t *testing.T) {
	file := dueFile(t)
	seedFile(t, file, "2026-07-14 buy milk\n")
	if code, _, errb := runDueCase(t, file, "1", "2026-07-20"); code != 0 {
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

// TestApplyDue unit-tests the token reconciliation directly, covering append,
// in-place replace, the multiple-due: first-replaced-rest-removed rule, and the
// `none` removal, independent of the CLI plumbing.
func TestApplyDue(t *testing.T) {
	cases := []struct {
		name string
		text string
		date string
		want string
	}{
		{"append when absent", "call the bank", "2026-07-20", "call the bank due:2026-07-20"},
		{"replace in place", "a due:2026-01-01 b", "2026-07-20", "a due:2026-07-20 b"},
		{"replace first drop rest", "a due:2026-01-01 b due:2026-02-02 c", "2026-07-20", "a due:2026-07-20 b c"},
		{"remove single", "a due:2026-01-01 b", "", "a b"},
		{"remove first and rest", "a due:2026-01-01 due:2026-02-02 b", "", "a b"},
		{"remove when absent is unchanged", "a b c", "", "a b c"},
		{"append leaves existing tokens", "a +proj @ctx", "2026-07-20", "a +proj @ctx due:2026-07-20"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := applyDue(c.text, c.date); got != c.want {
				t.Errorf("applyDue(%q, %q) = %q, want %q", c.text, c.date, got, c.want)
			}
		})
	}
}
