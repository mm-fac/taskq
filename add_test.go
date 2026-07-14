package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// addFile returns a --file path inside a fresh temp dir for an add test, so
// each case writes to its own isolated task file.
func addFile(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "todo.txt")
}

// runAddCase invokes the real dispatch (run) for `add` with a pinned --today,
// returning the exit code and captured streams. Going through run exercises the
// registered command, the global-flag parsing, and the today injection exactly
// as the binary would.
func runAddCase(t *testing.T, file string, args ...string) (int, string, string) {
	t.Helper()
	full := append([]string{"--today", "2026-07-14", "--file", file, "add"}, args...)
	var out, errb bytes.Buffer
	code := run(full, &out, &errb, noEnv)
	return code, out.String(), errb.String()
}

// readFile returns the file's contents, failing the test on any read error.
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	return string(data)
}

// TestAddCreatesMissingFile covers the happy path against a previously missing
// task file: the joined text gets today's creation date prepended, the file is
// created with exactly one trailing newline, and the printed line carries the
// new 1-based number byte-for-byte.
func TestAddCreatesMissingFile(t *testing.T) {
	file := addFile(t)
	code, out, errb := runAddCase(t, file, "buy", "milk")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr %q)", code, errb)
	}
	if want := "1 2026-07-14 buy milk\n"; out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
	if errb != "" {
		t.Errorf("stderr = %q, want empty", errb)
	}
	if want := "2026-07-14 buy milk\n"; readFile(t, file) != want {
		t.Errorf("file = %q, want %q", readFile(t, file), want)
	}
}

// TestAddJoinsWithSingleSpaces asserts multiple arguments are joined with
// exactly one space regardless of how the shell split them.
func TestAddJoinsWithSingleSpaces(t *testing.T) {
	file := addFile(t)
	code, out, _ := runAddCase(t, file, "call", "the", "bank")
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if want := "1 2026-07-14 call the bank\n"; out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
}

// TestAddLeadingPriority covers the leading `(A) `–`(Z) ` token being parsed as
// the priority and kept ahead of the creation date per the grammar.
func TestAddLeadingPriority(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"priority A", []string{"(A)", "call", "the", "bank"}, "1 (A) 2026-07-14 call the bank\n"},
		{"priority Z", []string{"(Z)", "someday"}, "1 (Z) 2026-07-14 someday\n"},
		{"single joined arg", []string{"(B) file taxes"}, "1 (B) 2026-07-14 file taxes\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			file := addFile(t)
			code, out, errb := runAddCase(t, file, c.args...)
			if code != 0 {
				t.Fatalf("exit = %d, want 0 (stderr %q)", code, errb)
			}
			if out != c.want {
				t.Errorf("stdout = %q, want %q", out, c.want)
			}
		})
	}
}

// TestAddNonPriorityLeadingParen asserts tokens that only look like a priority
// are kept as ordinary text: a lowercase letter, a missing trailing space, a
// multi-letter tag, or a digit inside the parens are all part of the text.
func TestAddNonPriorityLeadingParen(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"lowercase", []string{"(a)", "nope"}, "1 2026-07-14 (a) nope\n"},
		{"no trailing space", []string{"(A)buy"}, "1 2026-07-14 (A)buy\n"},
		{"multi letter", []string{"(AB)", "x"}, "1 2026-07-14 (AB) x\n"},
		{"bare token no text", []string{"(A)"}, "1 2026-07-14 (A)\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			file := addFile(t)
			code, out, errb := runAddCase(t, file, c.args...)
			if code != 0 {
				t.Fatalf("exit = %d, want 0 (stderr %q)", code, errb)
			}
			if out != c.want {
				t.Errorf("stdout = %q, want %q", out, c.want)
			}
		})
	}
}

// TestAddAppendsAndNumbers checks that a second add appends after the first and
// the printed line number reflects the new total line count.
func TestAddAppendsAndNumbers(t *testing.T) {
	file := addFile(t)
	if code, _, errb := runAddCase(t, file, "first"); code != 0 {
		t.Fatalf("first add exit = %d, want 0 (stderr %q)", code, errb)
	}
	code, out, errb := runAddCase(t, file, "(A)", "second")
	if code != 0 {
		t.Fatalf("second add exit = %d, want 0 (stderr %q)", code, errb)
	}
	if want := "2 (A) 2026-07-14 second\n"; out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
	if want := "2026-07-14 first\n(A) 2026-07-14 second\n"; readFile(t, file) != want {
		t.Errorf("file = %q, want %q", readFile(t, file), want)
	}
}

// TestAddEmptyIsUsageError covers the empty-text cases: no args, an empty
// joined string, whitespace-only text, and a leading priority that consumes the
// whole input. Each is exit 1, prints a taskq:-prefixed diagnostic, and leaves
// no file behind.
func TestAddEmptyIsUsageError(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"no args", nil},
		{"single empty arg", []string{""}},
		{"whitespace only", []string{"   "}},
		{"priority then nothing", []string{"(A) "}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			file := addFile(t)
			code, out, errb := runAddCase(t, file, c.args...)
			if code != 1 {
				t.Errorf("exit = %d, want 1", code)
			}
			if out != "" {
				t.Errorf("stdout = %q, want empty", out)
			}
			if !bytes.HasPrefix([]byte(errb), []byte("taskq: ")) {
				t.Errorf("stderr = %q, want taskq: prefix", errb)
			}
			if _, err := os.Stat(file); !os.IsNotExist(err) {
				t.Errorf("file %q exists after empty add, want nothing written (err %v)", file, err)
			}
		})
	}
}

// TestAddPreservesMalformedAndTrailingNewline appends to a file that already
// holds malformed lines: the malformed lines survive byte-for-byte in place,
// the new task is appended last with the right number, the file ends in exactly
// one newline, and the once-per-command malformed note is emitted on stderr.
func TestAddPreservesMalformedAndTrailingNewline(t *testing.T) {
	file := addFile(t)
	// Two malformed lines (a blank line and a spaces-only line) around one
	// well-formed task; no trailing newline on the seed to prove Save normalises.
	seed := "\n   \n2026-07-14 buy milk"
	if err := os.WriteFile(file, []byte(seed), 0o644); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	code, out, errb := runAddCase(t, file, "(A)", "file taxes")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr %q)", code, errb)
	}
	// The seed has 3 lines, so the appended task is line 4.
	if want := "4 (A) 2026-07-14 file taxes\n"; out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
	if want := "taskq: skipped 2 malformed line(s)\n"; errb != want {
		t.Errorf("stderr = %q, want %q", errb, want)
	}
	want := "\n   \n2026-07-14 buy milk\n(A) 2026-07-14 file taxes\n"
	if got := readFile(t, file); got != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}

// TestAddAtomicNoTempLitter asserts the atomic write leaves no temp file behind
// in the task file's directory after a successful add.
func TestAddAtomicNoTempLitter(t *testing.T) {
	file := addFile(t)
	if code, _, errb := runAddCase(t, file, "buy milk"); code != 0 {
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
