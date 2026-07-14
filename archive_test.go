package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// archiveFile returns a --file path inside a fresh temp dir so each archive test
// mutates its own isolated task file (and its own done.txt beside it).
func archiveFile(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "todo.txt")
}

// donePathFor returns the done.txt beside the given task file, matching the
// CLI's deriveDonePath (same directory as the task file).
func donePathFor(file string) string {
	return filepath.Join(filepath.Dir(file), "done.txt")
}

// runArchiveCase invokes the real dispatch (run) for `archive` with a pinned
// --today, returning the exit code and captured streams. Going through run
// exercises the registered command and global-flag parsing as the binary would.
// (archive writes no dates, but --today is pinned for hermeticity.)
func runArchiveCase(t *testing.T, file string, args ...string) (int, string, string) {
	t.Helper()
	full := append([]string{"--today", "2026-07-15", "--file", file, "archive"}, args...)
	var out, errb bytes.Buffer
	code := run(full, &out, &errb, noEnv)
	return code, out.String(), errb.String()
}

// skipIfRoot skips a test that relies on filesystem permissions, since root
// bypasses the mode bits the test depends on.
func skipIfRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("test relies on filesystem permissions; skipped when running as root")
	}
}

// TestArchiveHappyPath covers moving completed tasks out of the task file: they
// are appended to done.txt in file order, the task file is rewritten without
// them (open tasks keep their order), and both files end in exactly one trailing
// newline. done.txt is created when absent.
func TestArchiveHappyPath(t *testing.T) {
	cases := []struct {
		name     string
		seed     string
		wantOut  string
		wantFile string
		wantDone string
	}{
		{
			name:     "single completed task, file emptied",
			seed:     "x 2026-07-15 2026-07-14 buy milk\n",
			wantOut:  "archived 1 task(s)\n",
			wantFile: "",
			wantDone: "x 2026-07-15 2026-07-14 buy milk\n",
		},
		{
			name: "mixed open and completed keep their orders",
			seed: "2026-07-14 first open\n" +
				"x 2026-07-13 2026-07-10 done one\n" +
				"(A) 2026-07-14 second open\n" +
				"x 2026-07-12 2026-07-09 done two\n",
			wantOut:  "archived 2 task(s)\n",
			wantFile: "2026-07-14 first open\n(A) 2026-07-14 second open\n",
			wantDone: "x 2026-07-13 2026-07-10 done one\nx 2026-07-12 2026-07-09 done two\n",
		},
		{
			name:     "no completed tasks is a no-op",
			seed:     "2026-07-14 first\n(B) 2026-07-14 second\n",
			wantOut:  "archived 0 task(s)\n",
			wantFile: "2026-07-14 first\n(B) 2026-07-14 second\n",
			wantDone: "", // sentinel: done.txt must not exist (asserted below)
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			file := archiveFile(t)
			seedFile(t, file, c.seed)
			code, out, errb := runArchiveCase(t, file)
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
				t.Errorf("task file = %q, want %q", got, c.wantFile)
			}
			done := donePathFor(file)
			if c.wantOut == "archived 0 task(s)\n" {
				if _, err := os.Stat(done); !os.IsNotExist(err) {
					t.Errorf("done.txt exists after a no-op archive, want nothing written")
				}
				return
			}
			if got := readFile(t, done); got != c.wantDone {
				t.Errorf("done.txt = %q, want %q", got, c.wantDone)
			}
		})
	}
}

// TestArchiveAppendOrdering asserts step-1 appends to an EXISTING done.txt: the
// prior content is preserved and the newly archived tasks follow it, in file
// order, with exactly one trailing newline (no blank line introduced at the
// seam).
func TestArchiveAppendOrdering(t *testing.T) {
	file := archiveFile(t)
	seedFile(t, file,
		"x 2026-07-15 2026-07-14 third\n"+
			"2026-07-14 still open\n"+
			"x 2026-07-15 2026-07-14 fourth\n")
	// Pre-existing done.txt (e.g. from an earlier archive) ending in one newline.
	done := donePathFor(file)
	seedFile(t, done, "x 2026-07-01 2026-06-30 first\nx 2026-07-02 2026-07-01 second\n")

	code, out, errb := runArchiveCase(t, file)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr %q)", code, errb)
	}
	if want := "archived 2 task(s)\n"; out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
	if errb != "" {
		t.Errorf("stderr = %q, want empty", errb)
	}
	// New tasks are appended AFTER the existing ones, in file order.
	wantDone := "x 2026-07-01 2026-06-30 first\n" +
		"x 2026-07-02 2026-07-01 second\n" +
		"x 2026-07-15 2026-07-14 third\n" +
		"x 2026-07-15 2026-07-14 fourth\n"
	if got := readFile(t, done); got != wantDone {
		t.Errorf("done.txt = %q, want %q", got, wantDone)
	}
	// Exactly one trailing newline, no double newline anywhere at the seam.
	if raw := readFile(t, done); strings.Contains(raw, "\n\n") {
		t.Errorf("done.txt has a blank line / double newline: %q", raw)
	}
	// The task file is rewritten without the completed tasks; the open one stays.
	if got, want := readFile(t, file), "2026-07-14 still open\n"; got != want {
		t.Errorf("task file = %q, want %q", got, want)
	}
}

// TestArchivePreservesMalformed archives a file that also holds malformed lines:
// the completed tasks move to done.txt while the malformed lines survive
// byte-for-byte in place, the file ends in one trailing newline, and the
// once-per-command malformed note is emitted on stderr.
func TestArchivePreservesMalformed(t *testing.T) {
	file := archiveFile(t)
	// line 1 blank (malformed), 2 completed, 3 spaces-only (malformed), 4 open;
	// no trailing newline on the seed to prove Save normalises to exactly one.
	seedFile(t, file, "\nx 2026-07-15 2026-07-14 done\n   \n2026-07-14 open")

	code, out, errb := runArchiveCase(t, file)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr %q)", code, errb)
	}
	if want := "archived 1 task(s)\n"; out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
	if want := "taskq: skipped 2 malformed line(s)\n"; errb != want {
		t.Errorf("stderr = %q, want %q", errb, want)
	}
	// Malformed lines stay in place; the completed task is gone; one trailing \n.
	if got, want := readFile(t, file), "\n   \n2026-07-14 open\n"; got != want {
		t.Errorf("task file = %q, want %q", got, want)
	}
	if got, want := readFile(t, donePathFor(file)), "x 2026-07-15 2026-07-14 done\n"; got != want {
		t.Errorf("done.txt = %q, want %q", got, want)
	}
}

// TestArchiveEmptyNoOpNoWrites asserts the N=0 no-op performs NO file writes at
// all: with the task file's directory made read-only, archive still succeeds
// (proving it neither created done.txt nor rewrote the task file — either would
// have failed against the read-only directory), and done.txt is absent
// afterward.
func TestArchiveEmptyNoOpNoWrites(t *testing.T) {
	skipIfRoot(t)
	file := archiveFile(t)
	seedFile(t, file, "2026-07-14 open one\n2026-07-14 open two\n")
	dir := filepath.Dir(file)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("chmod dir read-only: %v", err)
	}
	defer os.Chmod(dir, 0o755) // restore so t.TempDir cleanup can remove it

	code, out, errb := runArchiveCase(t, file)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr %q) — a write was attempted", code, errb)
	}
	if want := "archived 0 task(s)\n"; out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
	if errb != "" {
		t.Errorf("stderr = %q, want empty", errb)
	}
	if _, err := os.Stat(donePathFor(file)); !os.IsNotExist(err) {
		t.Errorf("done.txt exists after a no-op archive, want nothing written")
	}
}

// TestArchiveMissingFileIsNoOp asserts that archive against a missing task file
// is not an error (archive addresses no task by number): it is a 0-task no-op
// that writes nothing, so done.txt is not created.
func TestArchiveMissingFileIsNoOp(t *testing.T) {
	file := archiveFile(t) // never created
	code, out, errb := runArchiveCase(t, file)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr %q)", code, errb)
	}
	if want := "archived 0 task(s)\n"; out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
	if errb != "" {
		t.Errorf("stderr = %q, want empty", errb)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("task file created by a no-op archive, want nothing written")
	}
	if _, err := os.Stat(donePathFor(file)); !os.IsNotExist(err) {
		t.Errorf("done.txt created by a no-op archive, want nothing written")
	}
}

// TestArchiveOrderingStep1BeforeStep2 asserts the decided ordering by injecting
// a step-2 failure: with the directory read-only but a writable done.txt already
// present, step 1 (append to the existing done.txt) succeeds while step 2
// (rewrite the task file, which needs a temp file in the read-only dir) fails.
// The result is the accepted failure mode — the completed tasks are now in BOTH
// files (duplicated, never lost) — which is only possible if step 1 ran first.
func TestArchiveOrderingStep1BeforeStep2(t *testing.T) {
	skipIfRoot(t)
	file := archiveFile(t)
	seed := "x 2026-07-15 2026-07-14 done one\n2026-07-14 open\n"
	seedFile(t, file, seed)
	// A writable, pre-existing done.txt so step 1 can append without needing to
	// create a file in the (about-to-be) read-only directory.
	done := donePathFor(file)
	seedFile(t, done, "x 2026-07-01 2026-06-30 earlier\n")

	dir := filepath.Dir(file)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("chmod dir read-only: %v", err)
	}
	defer os.Chmod(dir, 0o755)

	code, out, errb := runArchiveCase(t, file)
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (I/O failure); stderr %q", code, errb)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty on failure", out)
	}
	if !strings.HasPrefix(errb, "taskq: ") {
		t.Errorf("stderr = %q, want taskq: prefix", errb)
	}
	// Step 1 completed: the completed task was appended to done.txt after the
	// existing content.
	wantDone := "x 2026-07-01 2026-06-30 earlier\nx 2026-07-15 2026-07-14 done one\n"
	if got := readFile(t, done); got != wantDone {
		t.Errorf("done.txt = %q, want %q (step 1 must have appended before step 2 failed)", got, wantDone)
	}
	// Step 2 did not complete: the task file is unchanged, so the completed task
	// still lives there too — duplicated across both files, never lost.
	if got := readFile(t, file); got != seed {
		t.Errorf("task file = %q, want unchanged %q (step 2 failed, must not have rewritten)", got, seed)
	}
}

// TestArchiveHelp asserts `archive --help` (and -h) prints usage documenting the
// accepted failure mode (a crash between the two steps may duplicate but never
// lose a task), to stdout, with exit 0.
func TestArchiveHelp(t *testing.T) {
	for _, flagArg := range []string{"--help", "-h"} {
		t.Run(flagArg, func(t *testing.T) {
			file := archiveFile(t)
			seedFile(t, file, "x 2026-07-15 2026-07-14 done\n")
			code, out, errb := runArchiveCase(t, file, flagArg)
			if code != 0 {
				t.Fatalf("exit = %d, want 0 (stderr %q)", code, errb)
			}
			// The failure-mode documentation must mention duplication and no loss.
			for _, want := range []string{"duplicate", "never lose", "done.txt"} {
				if !strings.Contains(out, want) {
					t.Errorf("help text missing %q; got:\n%s", want, out)
				}
			}
			// --help must not archive anything: the file is untouched, done.txt absent.
			if got, want := readFile(t, file), "x 2026-07-15 2026-07-14 done\n"; got != want {
				t.Errorf("task file = %q, want unchanged %q", got, want)
			}
			if _, err := os.Stat(donePathFor(file)); !os.IsNotExist(err) {
				t.Errorf("done.txt created by --help, want nothing written")
			}
		})
	}
}

// TestArchiveUsageErrors covers usage-class failures (exit 1): an unexpected
// positional argument and an unknown flag. Each writes nothing to stdout and
// leaves both files untouched.
func TestArchiveUsageErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"unexpected argument", []string{"extra"}},
		{"unknown flag", []string{"--nope"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			file := archiveFile(t)
			seedFile(t, file, "x 2026-07-15 2026-07-14 done\n")
			code, out, errb := runArchiveCase(t, file, c.args...)
			if code != 1 {
				t.Errorf("exit = %d, want 1 (stderr %q)", code, errb)
			}
			if out != "" {
				t.Errorf("stdout = %q, want empty", out)
			}
			if !strings.HasPrefix(errb, "taskq: ") {
				t.Errorf("stderr = %q, want taskq: prefix", errb)
			}
			if got, want := readFile(t, file), "x 2026-07-15 2026-07-14 done\n"; got != want {
				t.Errorf("task file = %q, want unchanged %q", got, want)
			}
			if _, err := os.Stat(donePathFor(file)); !os.IsNotExist(err) {
				t.Errorf("done.txt created by a failed archive, want nothing written")
			}
		})
	}
}

// TestArchiveAtomicNoTempLitter asserts the step-2 atomic rewrite leaves no temp
// file behind in the task file's directory after a successful archive (only the
// task file and done.txt remain).
func TestArchiveAtomicNoTempLitter(t *testing.T) {
	file := archiveFile(t)
	seedFile(t, file, "x 2026-07-15 2026-07-14 done\n2026-07-14 open\n")
	if code, _, errb := runArchiveCase(t, file); code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr %q)", code, errb)
	}
	entries, err := os.ReadDir(filepath.Dir(file))
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "todo.txt" && e.Name() != "done.txt" {
			t.Errorf("unexpected leftover file %q in task dir", e.Name())
		}
	}
}
