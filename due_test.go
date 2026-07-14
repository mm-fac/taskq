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

// TestDueHappyPath covers append, in-place replace, the multiple-`due:` case,
// and `none`: the addressed line's due token is set, replaced, or removed while
// the surrounding text is untouched, the printed line carries its number, and
// the file ends with exactly one trailing newline.
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
			name:     "append preserves trailing token then adds due",
			seed:     "2026-07-14 call the bank +errands @phone\n",
			args:     []string{"1", "2026-07-20"},
			wantOut:  "1 2026-07-14 call the bank +errands @phone due:2026-07-20\n",
			wantFile: "2026-07-14 call the bank +errands @phone due:2026-07-20\n",
		},
		{
			name:     "replace existing due in place",
			seed:     "2026-07-14 call the bank due:2026-07-20 +errands\n",
			args:     []string{"1", "2026-07-25"},
			wantOut:  "1 2026-07-14 call the bank due:2026-07-25 +errands\n",
			wantFile: "2026-07-14 call the bank due:2026-07-25 +errands\n",
		},
		{
			name:     "replace due at end of text",
			seed:     "2026-07-14 call the bank due:2026-07-20\n",
			args:     []string{"1", "2026-07-25"},
			wantOut:  "1 2026-07-14 call the bank due:2026-07-25\n",
			wantFile: "2026-07-14 call the bank due:2026-07-25\n",
		},
		{
			name:     "multiple due: replace first, remove rest",
			seed:     "2026-07-14 pay due:2026-01-01 rent due:2026-02-02 now\n",
			args:     []string{"1", "2026-07-25"},
			wantOut:  "1 2026-07-14 pay due:2026-07-25 rent now\n",
			wantFile: "2026-07-14 pay due:2026-07-25 rent now\n",
		},
		{
			name:     "multiple due: adjacent tokens replace first, remove rest",
			seed:     "2026-07-14 pay due:2026-01-01 due:2026-02-02 rent\n",
			args:     []string{"1", "2026-07-25"},
			wantOut:  "1 2026-07-14 pay due:2026-07-25 rent\n",
			wantFile: "2026-07-14 pay due:2026-07-25 rent\n",
		},
		{
			name:     "none removes the due token",
			seed:     "2026-07-14 call the bank due:2026-07-20 +errands\n",
			args:     []string{"1", "none"},
			wantOut:  "1 2026-07-14 call the bank +errands\n",
			wantFile: "2026-07-14 call the bank +errands\n",
		},
		{
			name:     "none removes a trailing due token without dangling space",
			seed:     "2026-07-14 call the bank due:2026-07-20\n",
			args:     []string{"1", "none"},
			wantOut:  "1 2026-07-14 call the bank\n",
			wantFile: "2026-07-14 call the bank\n",
		},
		{
			name:     "none removes all of multiple due tokens",
			seed:     "2026-07-14 pay due:2026-01-01 rent due:2026-02-02 now\n",
			args:     []string{"1", "none"},
			wantOut:  "1 2026-07-14 pay rent now\n",
			wantFile: "2026-07-14 pay rent now\n",
		},
		{
			name:     "none is a no-op when there is no due token",
			seed:     "2026-07-14 buy milk\n",
			args:     []string{"1", "none"},
			wantOut:  "1 2026-07-14 buy milk\n",
			wantFile: "2026-07-14 buy milk\n",
		},
		{
			name:     "uppercase NONE also removes the due token",
			seed:     "2026-07-14 buy milk due:2026-07-20\n",
			args:     []string{"1", "NONE"},
			wantOut:  "1 2026-07-14 buy milk\n",
			wantFile: "2026-07-14 buy milk\n",
		},
		{
			name:     "append on task without creation date",
			seed:     "water plants\n",
			args:     []string{"1", "2026-07-20"},
			wantOut:  "1 water plants due:2026-07-20\n",
			wantFile: "water plants due:2026-07-20\n",
		},
		{
			name:     "prioritized task keeps its priority",
			seed:     "(A) 2026-07-14 call the bank due:2026-07-20\n",
			args:     []string{"1", "2026-07-25"},
			wantOut:  "1 (A) 2026-07-14 call the bank due:2026-07-25\n",
			wantFile: "(A) 2026-07-14 call the bank due:2026-07-25\n",
		},
		{
			name:     "second of two tasks",
			seed:     "2026-07-14 first\n2026-07-14 second due:2026-07-20\n",
			args:     []string{"2", "2026-07-25"},
			wantOut:  "2 2026-07-14 second due:2026-07-25\n",
			wantFile: "2026-07-14 first\n2026-07-14 second due:2026-07-25\n",
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

// TestDueEmptyTextRemovalRejected covers the owner decision: `due <n> none`
// where the due: token(s) are the task's entire text would leave the text
// empty, so it is a usage error (exit 1) that writes nothing and leaves the
// file byte-identical.
func TestDueEmptyTextRemovalRejected(t *testing.T) {
	cases := []struct {
		name string
		seed string
	}{
		{"single due is whole text", "due:2026-07-20\n"},
		{"single due with creation date", "2026-07-14 due:2026-07-20\n"},
		{"multiple due are whole text", "2026-07-14 due:2026-07-20 due:2026-08-01\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			file := dueFile(t)
			seedFile(t, file, c.seed)
			code, out, errb := runDueCase(t, file, "1", "none")
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
				t.Errorf("file = %q, want byte-identical %q", got, c.seed)
			}
		})
	}
}

// TestDueUsageErrors covers the usage-class failures (exit 1): a bad date
// argument, a number out of range, a number landing on a malformed line, a
// non-numeric number, and the wrong argument count. Each writes nothing to
// stdout, emits a taskq:-prefixed diagnostic, and leaves the file unchanged.
func TestDueUsageErrors(t *testing.T) {
	cases := []struct {
		name string
		seed string
		args []string
	}{
		{"date not zero-padded", "2026-07-14 real\n", []string{"1", "2026-7-14"}},
		{"date not calendar-valid", "2026-07-14 real\n", []string{"1", "2026-02-30"}},
		{"date month out of range", "2026-07-14 real\n", []string{"1", "2026-13-01"}},
		{"date empty", "2026-07-14 real\n", []string{"1", ""}},
		{"date word", "2026-07-14 real\n", []string{"1", "soon"}},
		{"date with due prefix", "2026-07-14 real\n", []string{"1", "due:2026-07-20"}},
		{"out of range high", "2026-07-14 only task\n", []string{"2", "2026-07-20"}},
		{"out of range zero", "2026-07-14 only task\n", []string{"0", "2026-07-20"}},
		{"negative", "2026-07-14 only task\n", []string{"-1", "2026-07-20"}},
		{"malformed target", "\n2026-07-14 real\n", []string{"1", "2026-07-20"}},
		{"non-numeric number", "2026-07-14 real\n", []string{"abc", "2026-07-20"}},
		{"no args", "2026-07-14 real\n", nil},
		{"one arg", "2026-07-14 real\n", []string{"1"}},
		{"too many args", "2026-07-14 real\n", []string{"1", "2026-07-20", "x"}},
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

// TestDueMissingFileIsIOError asserts that addressing a task by number against
// a missing task file is an I/O error (exit 2), not a usage error, and creates
// no file — even though the date argument itself is well-formed.
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
// place, the target is re-dated, the file ends in exactly one newline, and the
// once-per-command malformed note is emitted on stderr alongside the resulting
// line's stdout.
func TestDuePreservesMalformedAndTrailingNewline(t *testing.T) {
	file := dueFile(t)
	// A blank line and a spaces-only line (both malformed) around two tasks; no
	// trailing newline on the seed to prove Save normalises to exactly one.
	seed := "\n2026-07-14 call the bank due:2026-07-20\n   \n2026-07-14 buy milk"
	seedFile(t, file, seed)

	// Line 4 is the second task (lines 1 and 3 are malformed).
	code, out, errb := runDueCase(t, file, "4", "2026-07-25")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr %q)", code, errb)
	}
	if want := "4 2026-07-14 buy milk due:2026-07-25\n"; out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
	if want := "taskq: skipped 2 malformed line(s)\n"; errb != want {
		t.Errorf("stderr = %q, want %q", errb, want)
	}
	want := "\n2026-07-14 call the bank due:2026-07-20\n   \n2026-07-14 buy milk due:2026-07-25\n"
	if got := readFile(t, file); got != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}

// TestDueAtomicNoTempLitter asserts the atomic write leaves no temp file behind
// in the task file's directory after a successful re-dating.
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
