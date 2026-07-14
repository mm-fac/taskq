package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// mixedFixture is the committed fixture testdata/mixed.txt. It holds six lines
// in this exact order, ending in a single '\n':
//
//	1  (A) 2026-07-14 call the bank +errands @phone due:2026-07-20   well-formed
//	2  x 2026-07-15 2026-07-14 write the report                      well-formed (done)
//	3  (empty line)                                                  malformed
//	4  2026-07-14 buy milk +home @store                              well-formed
//	5  "   " (three spaces)                                          malformed
//	6  (B) 2026-07-10 file taxes                                     well-formed
const mixedFixture = "testdata/mixed.txt"

// TestLoadFixture checks that Load reads every line in order and counts the
// malformed ones, using the committed fixture.
func TestLoadFixture(t *testing.T) {
	tasks, malformed, err := Load(mixedFixture)
	if err != nil {
		t.Fatalf("Load(%q) error: %v", mixedFixture, err)
	}
	if len(tasks) != 6 {
		t.Fatalf("Load returned %d lines, want 6", len(tasks))
	}
	if malformed != 2 {
		t.Errorf("Load malformed count = %d, want 2", malformed)
	}

	wantRaw := []string{
		"(A) 2026-07-14 call the bank +errands @phone due:2026-07-20",
		"x 2026-07-15 2026-07-14 write the report",
		"",
		"2026-07-14 buy milk +home @store",
		"   ",
		"(B) 2026-07-10 file taxes",
	}
	wantMalformed := []bool{false, false, true, false, true, false}
	for i, t2 := range tasks {
		if t2.Raw != wantRaw[i] {
			t.Errorf("line %d Raw = %q, want %q", i+1, t2.Raw, wantRaw[i])
		}
		if t2.Malformed != wantMalformed[i] {
			t.Errorf("line %d Malformed = %v, want %v", i+1, t2.Malformed, wantMalformed[i])
		}
	}
}

// TestLoadMissingFile asserts an absent path yields an empty list with no error
// (read semantics: callers decide whether that is legal).
func TestLoadMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.txt")
	tasks, malformed, err := Load(path)
	if err != nil {
		t.Fatalf("Load(missing) error = %v, want nil", err)
	}
	if len(tasks) != 0 {
		t.Errorf("Load(missing) returned %d lines, want 0", len(tasks))
	}
	if malformed != 0 {
		t.Errorf("Load(missing) malformed = %d, want 0", malformed)
	}
}

// TestLoadEmptyFile checks that an existing zero-byte file is an empty task
// list (distinct from a missing file, which matters for mutations).
func TestLoadEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "todo.txt")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	tasks, malformed, err := Load(path)
	if err != nil {
		t.Fatalf("Load(empty) error: %v", err)
	}
	if len(tasks) != 0 || malformed != 0 {
		t.Errorf("Load(empty) = (%d lines, %d malformed), want (0, 0)", len(tasks), malformed)
	}
}

// TestSaveAtomicNoLeftoverTemp asserts a Save writes complete, correct content
// and leaves no temp file behind in the directory.
func TestSaveAtomicNoLeftoverTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "todo.txt")

	tasks := []Task{
		ParseLine("(A) 2026-07-14 alpha"),
		ParseLine("2026-07-14 beta +work"),
	}
	if err := Save(path, tasks); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "(A) 2026-07-14 alpha\n2026-07-14 beta +work\n"
	if string(got) != want {
		t.Errorf("content = %q, want %q", got, want)
	}

	// The directory must contain exactly the destination file: no ".taskq-*"
	// temp left over from the atomic rewrite.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "todo.txt" {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("directory entries = %v, want exactly [todo.txt]", names)
	}
}

// TestSaveTrailingNewline covers the trailing-newline rule: a non-empty list
// ends in exactly one '\n', and an empty list writes a zero-byte file.
func TestSaveTrailingNewline(t *testing.T) {
	dir := t.TempDir()

	single := filepath.Join(dir, "single.txt")
	if err := Save(single, []Task{ParseLine("2026-07-14 lonely task")}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(single); string(got) != "2026-07-14 lonely task\n" {
		t.Errorf("single-task file = %q, want one trailing newline", got)
	}

	empty := filepath.Join(dir, "empty.txt")
	if err := Save(empty, nil); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(empty)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() != 0 {
		t.Errorf("empty task list wrote %d bytes, want 0", fi.Size())
	}
}

// TestRewriteRoundTripPreservesMalformed loads the fixture (which contains two
// malformed lines) and rewrites it, asserting the destination is byte-for-byte
// identical to the source — malformed lines preserved in their positions.
func TestRewriteRoundTripPreservesMalformed(t *testing.T) {
	orig, err := os.ReadFile(mixedFixture)
	if err != nil {
		t.Fatal(err)
	}

	tasks, _, err := Load(mixedFixture)
	if err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "todo.txt")
	if err := Save(out, tasks); err != nil {
		t.Fatalf("Save error: %v", err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(orig) {
		t.Errorf("round-trip changed bytes:\n orig %q\n got  %q", orig, got)
	}
}

// TestResolve covers number resolution over ALL lines: valid targets, the
// out-of-range usage error, and the malformed-target usage error, using the
// fixture so line numbers match a text editor.
func TestResolve(t *testing.T) {
	tasks, _, err := Load(mixedFixture)
	if err != nil {
		t.Fatal(err)
	}

	// Valid, well-formed targets resolve to num-1.
	for _, num := range []int{1, 2, 4, 6} {
		idx, err := Resolve(tasks, num)
		if err != nil {
			t.Errorf("Resolve(%d) error = %v, want ok", num, err)
			continue
		}
		if idx != num-1 {
			t.Errorf("Resolve(%d) idx = %d, want %d", num, idx, num-1)
		}
	}

	// Out-of-range numbers are a RangeError (usage class).
	for _, num := range []int{0, -1, 7, 100} {
		_, err := Resolve(tasks, num)
		var re *RangeError
		if !errors.As(err, &re) {
			t.Errorf("Resolve(%d) error = %v, want *RangeError", num, err)
		}
	}

	// Numbers landing on a malformed line (3 and 5) are a MalformedTargetError.
	for _, num := range []int{3, 5} {
		_, err := Resolve(tasks, num)
		var me *MalformedTargetError
		if !errors.As(err, &me) {
			t.Errorf("Resolve(%d) error = %v, want *MalformedTargetError", num, err)
		}
	}
}

// TestDistinctErrorTypes asserts the three distinct, typed errors a
// number-addressing mutation can surface: missing file (I/O class) vs
// out-of-range (usage) vs malformed target (usage).
func TestDistinctErrorTypes(t *testing.T) {
	// Missing file => NoFileError from LoadForMutation.
	missing := filepath.Join(t.TempDir(), "gone.txt")
	_, _, err := LoadForMutation(missing)
	var nfe *NoFileError
	if !errors.As(err, &nfe) {
		t.Fatalf("LoadForMutation(missing) error = %v, want *NoFileError", err)
	}

	// An existing file loads without a NoFileError; out-of-range and malformed
	// targets then come from Resolve and are neither NoFileError nor each other.
	tasks, _, err := LoadForMutation(mixedFixture)
	if err != nil {
		t.Fatalf("LoadForMutation(fixture) error: %v", err)
	}

	_, rangeErr := Resolve(tasks, 99)
	_, malErr := Resolve(tasks, 3)

	var re *RangeError
	var me *MalformedTargetError
	if !errors.As(rangeErr, &re) {
		t.Errorf("out-of-range error = %v, want *RangeError", rangeErr)
	}
	if errors.As(rangeErr, &me) {
		t.Errorf("out-of-range error also matched *MalformedTargetError")
	}
	if !errors.As(malErr, &me) {
		t.Errorf("malformed-target error = %v, want *MalformedTargetError", malErr)
	}
	if errors.As(malErr, &re) {
		t.Errorf("malformed-target error also matched *RangeError")
	}
	// The missing-file error is neither usage-class error.
	if errors.As(err, &re) || errors.As(err, &me) {
		t.Errorf("NoFileError should not match a usage-class error type")
	}
}

// TestLoadForMutationEmptyFileNotMissing confirms an existing empty file is not
// a NoFileError: it exists, so a number is simply out of range (usage), which
// keeps the I/O-vs-usage distinction sharp.
func TestLoadForMutationEmptyFileNotMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "todo.txt")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	tasks, _, err := LoadForMutation(path)
	if err != nil {
		t.Fatalf("LoadForMutation(empty) error = %v, want nil", err)
	}
	if _, err := Resolve(tasks, 1); err == nil {
		t.Error("Resolve(1) on empty file = nil, want out-of-range error")
	}
}

// TestSaveIOErrorType asserts a write into a nonexistent directory is reported
// as an IOError (I/O class), distinct from the usage-class errors.
func TestSaveIOErrorType(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-such-dir", "todo.txt")
	err := Save(path, []Task{ParseLine("2026-07-14 x")})
	var ioe *IOError
	if !errors.As(err, &ioe) {
		t.Fatalf("Save into missing dir error = %v, want *IOError", err)
	}
}
