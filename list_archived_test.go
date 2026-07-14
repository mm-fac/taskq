package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// list_archived_test.go covers `list --archived`: reading the sibling done.txt
// (same directory as the resolved task file) instead of the task file, listing
// every well-formed line regardless of completion state, the missing-done.txt
// empty listing, the --all/--done conflicts, that the existing filters and
// sorts apply identically, and that the command performs no writes.
//
// The done.txt content is the committed fixture testdata/archived.txt, seeded
// into a temp directory alongside a distinct todo.txt so the tests also prove
// --archived reads done.txt and never the task file. --today is pinned via
// runListCase, keeping every case hermetic.

// archivedFixture reads the committed done.txt fixture (testdata/archived.txt).
// Its lines, by 1-based number within done.txt:
//
//	1  x 2026-07-15 2026-07-14 write the report +work        (completed)
//	2  (A) 2026-07-14 hand-edited open task +errands @phone due:2026-07-20 (open)
//	3  x 2026-07-16 2026-07-10 file taxes +home due:2026-07-01 (completed)
//	4  (blank — malformed)
//	5  x 2026-07-12 2026-07-11 pay rent +home                (completed)
//	6  (spaces only — malformed)
//	7  2026-07-13 another open line @desk                    (open)
func archivedFixture(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "archived.txt"))
	if err != nil {
		t.Fatalf("read archived fixture: %v", err)
	}
	return string(data)
}

// seedArchived writes doneContent to done.txt and a distinct, unrelated todo.txt
// in a fresh temp directory, returning the task-file path to pass as --file. The
// derived done.txt (deriveDonePath) sits beside it, so `list --archived` reads
// the seeded done.txt. The todo.txt content is intentionally different so a test
// asserting --archived output proves it ignored the task file.
func seedArchived(t *testing.T, doneContent string) (file, done string) {
	t.Helper()
	dir := t.TempDir()
	file = filepath.Join(dir, "todo.txt")
	done = filepath.Join(dir, "done.txt")
	if err := os.WriteFile(file, []byte("2026-07-14 an open task that lives in todo.txt only +todoonly\n"), 0o644); err != nil {
		t.Fatalf("seed todo write: %v", err)
	}
	if err := os.WriteFile(done, []byte(doneContent), 0o644); err != nil {
		t.Fatalf("seed done write: %v", err)
	}
	return file, done
}

// TestListArchivedDefault lists every well-formed line from done.txt regardless
// of completion state (completed lines 1/3/5 AND the hand-edited open lines 2/7),
// prefixed by the 1-based line number within done.txt (malformed lines 4 and 6
// skipped but still consuming their number), and reports the two malformed lines
// once on stderr. It also proves the task file's own +todoonly task is absent.
func TestListArchivedDefault(t *testing.T) {
	file, _ := seedArchived(t, archivedFixture(t))
	code, out, errb := runListCase(t, file, "--archived")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr %q)", code, errb)
	}
	want := "1 x 2026-07-15 2026-07-14 write the report +work\n" +
		"2 (A) 2026-07-14 hand-edited open task +errands @phone due:2026-07-20\n" +
		"3 x 2026-07-16 2026-07-10 file taxes +home due:2026-07-01\n" +
		"5 x 2026-07-12 2026-07-11 pay rent +home\n" +
		"7 2026-07-13 another open line @desk\n"
	if out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
	if want := "taskq: skipped 2 malformed line(s)\n"; errb != want {
		t.Errorf("stderr = %q, want %q", errb, want)
	}
}

// TestListArchivedMissingDone covers a missing done.txt: an empty listing, exit
// 0, no stderr, and — crucially — no done.txt is created (read-only command).
func TestListArchivedMissingDone(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	if err := os.WriteFile(file, []byte("2026-07-14 present but irrelevant\n"), 0o644); err != nil {
		t.Fatalf("seed todo write: %v", err)
	}
	done := filepath.Join(dir, "done.txt")

	code, out, errb := runListCase(t, file, "--archived")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr %q)", code, errb)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if errb != "" {
		t.Errorf("stderr = %q, want empty", errb)
	}
	if _, err := os.Stat(done); !os.IsNotExist(err) {
		t.Errorf("done.txt should not exist after a read-only list --archived; stat err = %v", err)
	}
}

// TestListArchivedConflicts asserts --archived cannot be combined with --all or
// with --done: each is a usage error (exit 1) with a taskq:-prefixed diagnostic
// and nothing on stdout.
func TestListArchivedConflicts(t *testing.T) {
	file, _ := seedArchived(t, archivedFixture(t))
	for _, flag := range []string{"--all", "--done"} {
		t.Run(flag, func(t *testing.T) {
			code, out, errb := runListCase(t, file, "--archived", flag)
			if code != 1 {
				t.Errorf("exit = %d, want 1", code)
			}
			if out != "" {
				t.Errorf("stdout = %q, want empty", out)
			}
			if !bytes.HasPrefix([]byte(errb), []byte("taskq: ")) {
				t.Errorf("stderr = %q, want taskq: prefix", errb)
			}
		})
	}
}

// TestListArchivedFilters asserts the existing --project/--context/--overdue
// filters apply to --archived output identically, matched against the committed
// done.txt fixture with a pinned today of 2026-07-14.
func TestListArchivedFilters(t *testing.T) {
	file, _ := seedArchived(t, archivedFixture(t))
	cases := []struct {
		name    string
		args    []string
		wantOut string
	}{
		{
			name: "project home keeps completed lines 3 and 5",
			args: []string{"--project", "home"},
			wantOut: "3 x 2026-07-16 2026-07-10 file taxes +home due:2026-07-01\n" +
				"5 x 2026-07-12 2026-07-11 pay rent +home\n",
		},
		{
			name:    "context phone keeps the open line 2",
			args:    []string{"--context", "phone"},
			wantOut: "2 (A) 2026-07-14 hand-edited open task +errands @phone due:2026-07-20\n",
		},
		{
			name:    "overdue keeps only the past-due line 3",
			args:    []string{"--overdue"},
			wantOut: "3 x 2026-07-16 2026-07-10 file taxes +home due:2026-07-01\n",
		},
		{
			name:    "project home AND overdue",
			args:    []string{"--project", "home", "--overdue"},
			wantOut: "3 x 2026-07-16 2026-07-10 file taxes +home due:2026-07-01\n",
		},
		{
			name:    "context case sensitive no match",
			args:    []string{"--context", "Phone"},
			wantOut: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			args := append([]string{"--archived"}, c.args...)
			code, out, _ := runListCase(t, file, args...)
			if code != 0 {
				t.Fatalf("exit = %d, want 0", code)
			}
			if out != c.wantOut {
				t.Errorf("stdout = %q, want %q", out, c.wantOut)
			}
		})
	}
}

// TestListArchivedSorts asserts the existing --sort keys apply to --archived
// output identically, with the done.txt line numbers riding along through the
// stable reorder.
func TestListArchivedSorts(t *testing.T) {
	file, _ := seedArchived(t, archivedFixture(t))
	cases := []struct {
		name    string
		sortKey string
		wantOut string
	}{
		{
			// Only line 2 carries a priority (A); the rest are unprioritized and
			// keep file order after it.
			name:    "pri",
			sortKey: "pri",
			wantOut: "2 (A) 2026-07-14 hand-edited open task +errands @phone due:2026-07-20\n" +
				"1 x 2026-07-15 2026-07-14 write the report +work\n" +
				"3 x 2026-07-16 2026-07-10 file taxes +home due:2026-07-01\n" +
				"5 x 2026-07-12 2026-07-11 pay rent +home\n" +
				"7 2026-07-13 another open line @desk\n",
		},
		{
			// Due dates: line 3 (07-01) earliest, then line 2 (07-20); lines
			// 1/5/7 have no due: and keep file order last.
			name:    "due",
			sortKey: "due",
			wantOut: "3 x 2026-07-16 2026-07-10 file taxes +home due:2026-07-01\n" +
				"2 (A) 2026-07-14 hand-edited open task +errands @phone due:2026-07-20\n" +
				"1 x 2026-07-15 2026-07-14 write the report +work\n" +
				"5 x 2026-07-12 2026-07-11 pay rent +home\n" +
				"7 2026-07-13 another open line @desk\n",
		},
		{
			// Creation dates: 3 (07-10), 5 (07-11), 7 (07-13), then the two
			// 07-14 tasks (1, 2) in file order.
			name:    "created",
			sortKey: "created",
			wantOut: "3 x 2026-07-16 2026-07-10 file taxes +home due:2026-07-01\n" +
				"5 x 2026-07-12 2026-07-11 pay rent +home\n" +
				"7 2026-07-13 another open line @desk\n" +
				"1 x 2026-07-15 2026-07-14 write the report +work\n" +
				"2 (A) 2026-07-14 hand-edited open task +errands @phone due:2026-07-20\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, out, _ := runListCase(t, file, "--archived", "--sort", c.sortKey)
			if code != 0 {
				t.Fatalf("exit = %d, want 0", code)
			}
			if out != c.wantOut {
				t.Errorf("stdout = %q, want %q", out, c.wantOut)
			}
		})
	}
}

// TestListArchivedNoWrites asserts `list --archived` performs no writes: the
// done.txt, the task file, and the directory listing are byte-identical before
// and after the command (no rewrite, no stray temp files).
func TestListArchivedNoWrites(t *testing.T) {
	file, done := seedArchived(t, archivedFixture(t))
	dir := filepath.Dir(file)

	before := dirSnapshot(t, dir)
	code, _, _ := runListCase(t, file, "--archived", "--sort", "due", "--project", "home")
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	after := dirSnapshot(t, dir)

	if len(before) != len(after) {
		t.Fatalf("directory entry set changed: before %v, after %v", keys(before), keys(after))
	}
	for name, content := range before {
		got, ok := after[name]
		if !ok {
			t.Errorf("file %q disappeared", name)
			continue
		}
		if got != content {
			t.Errorf("file %q changed:\n before %q\n after  %q", name, content, got)
		}
	}
	// Belt and suspenders: the fixture done.txt is unchanged verbatim.
	if got := after[filepath.Base(done)]; got != archivedFixture(t) {
		t.Errorf("done.txt content changed: %q", got)
	}
}

// dirSnapshot maps each regular file's base name in dir to its full content, so
// a test can prove a command left the directory byte-for-byte unchanged.
func dirSnapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	snap := make(map[string]string, len(ents))
	for _, e := range ents {
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		snap[e.Name()] = string(data)
	}
	return snap
}

// keys returns the sorted-insensitive key set of a snapshot for diagnostics.
func keys(m map[string]string) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
