package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// seedList writes content to a fresh temp task file and returns its path, so
// each list test reads from its own isolated, hermetic file.
func seedList(t *testing.T, content string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "todo.txt")
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatalf("seed write: %v", err)
	}
	return file
}

// runListCase invokes the real dispatch (run) for `list` with a pinned --today
// against file, returning the exit code and captured streams. Going through run
// exercises the registered command, global-flag parsing, and today injection.
func runListCase(t *testing.T, file string, args ...string) (int, string, string) {
	t.Helper()
	full := append([]string{"--today", "2026-07-14", "--file", file, "list"}, args...)
	var out, errb bytes.Buffer
	code := run(full, &out, &errb, noEnv)
	return code, out.String(), errb.String()
}

// listFixture is a task file exercising every code path: an open prioritized
// task, a completed task, a malformed blank line (line 3), an open task, a
// malformed spaces-only line (line 5), and a lower-priority open task with an
// earlier creation and due date. Line numbers below refer to this layout.
//
//	1  (A) 2026-07-14 call the bank +errands @phone due:2026-07-20
//	2  x 2026-07-15 2026-07-14 write the report
//	3  (blank — malformed)
//	4  2026-07-14 buy milk +home @store
//	5  (spaces only — malformed)
//	6  (B) 2026-07-10 file taxes +home due:2026-07-01
const listFixture = "(A) 2026-07-14 call the bank +errands @phone due:2026-07-20\n" +
	"x 2026-07-15 2026-07-14 write the report\n" +
	"\n" +
	"2026-07-14 buy milk +home @store\n" +
	"   \n" +
	"(B) 2026-07-10 file taxes +home due:2026-07-01\n"

// TestListMissingFileEmpty covers the missing-file case: an empty listing and
// exit 0, with no stderr (no malformed lines to report either).
func TestListMissingFileEmpty(t *testing.T) {
	file := filepath.Join(t.TempDir(), "todo.txt")
	code, out, errb := runListCase(t, file)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr %q)", code, errb)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if errb != "" {
		t.Errorf("stderr = %q, want empty", errb)
	}
}

// TestListEmptyFileEmpty covers a zero-byte task file: still an empty listing,
// exit 0, no diagnostics.
func TestListEmptyFileEmpty(t *testing.T) {
	file := seedList(t, "")
	code, out, errb := runListCase(t, file)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr %q)", code, errb)
	}
	if out != "" || errb != "" {
		t.Errorf("stdout = %q, stderr = %q, want both empty", out, errb)
	}
}

// TestListScopes covers the three scopes over the fixture, asserting exact
// stdout and that the once-per-command malformed note (2 malformed lines) is
// emitted on stderr in every case. Line numbers reflect the file identity, with
// malformed lines 3 and 5 skipped from output but still consuming their number.
func TestListScopes(t *testing.T) {
	file := seedList(t, listFixture)
	cases := []struct {
		name    string
		args    []string
		wantOut string
	}{
		{
			name: "default open only",
			args: nil,
			wantOut: "1 (A) 2026-07-14 call the bank +errands @phone due:2026-07-20\n" +
				"4 2026-07-14 buy milk +home @store\n" +
				"6 (B) 2026-07-10 file taxes +home due:2026-07-01\n",
		},
		{
			name: "all open plus completed",
			args: []string{"--all"},
			wantOut: "1 (A) 2026-07-14 call the bank +errands @phone due:2026-07-20\n" +
				"2 x 2026-07-15 2026-07-14 write the report\n" +
				"4 2026-07-14 buy milk +home @store\n" +
				"6 (B) 2026-07-10 file taxes +home due:2026-07-01\n",
		},
		{
			name:    "done completed only",
			args:    []string{"--done"},
			wantOut: "2 x 2026-07-15 2026-07-14 write the report\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, out, errb := runListCase(t, file, c.args...)
			if code != 0 {
				t.Fatalf("exit = %d, want 0 (stderr %q)", code, errb)
			}
			if out != c.wantOut {
				t.Errorf("stdout = %q, want %q", out, c.wantOut)
			}
			if want := "taskq: skipped 2 malformed line(s)\n"; errb != want {
				t.Errorf("stderr = %q, want %q", errb, want)
			}
		})
	}
}

// TestListAllDoneConflict asserts --all with --done is a usage error (exit 1)
// with a taskq:-prefixed diagnostic and nothing on stdout.
func TestListAllDoneConflict(t *testing.T) {
	file := seedList(t, listFixture)
	code, out, errb := runListCase(t, file, "--all", "--done")
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if !bytes.HasPrefix([]byte(errb), []byte("taskq: ")) {
		t.Errorf("stderr = %q, want taskq: prefix", errb)
	}
}

// TestListFilters covers each filter and their AND combination. --project and
// --context are exact, case-sensitive token matches; --overdue keeps tasks
// whose due: date is strictly before the pinned today (2026-07-14).
func TestListFilters(t *testing.T) {
	file := seedList(t, listFixture)
	cases := []struct {
		name    string
		args    []string
		wantOut string
	}{
		{
			name: "project home",
			args: []string{"--project", "home"},
			wantOut: "4 2026-07-14 buy milk +home @store\n" +
				"6 (B) 2026-07-10 file taxes +home due:2026-07-01\n",
		},
		{
			name:    "context phone",
			args:    []string{"--context", "phone"},
			wantOut: "1 (A) 2026-07-14 call the bank +errands @phone due:2026-07-20\n",
		},
		{
			name:    "project errands and context phone AND",
			args:    []string{"--project", "errands", "--context", "phone"},
			wantOut: "1 (A) 2026-07-14 call the bank +errands @phone due:2026-07-20\n",
		},
		{
			name:    "project errands and context store AND yields none",
			args:    []string{"--project", "errands", "--context", "store"},
			wantOut: "",
		},
		{
			name:    "overdue only",
			args:    []string{"--overdue"},
			wantOut: "6 (B) 2026-07-10 file taxes +home due:2026-07-01\n",
		},
		{
			name:    "overdue and project home AND",
			args:    []string{"--overdue", "--project", "home"},
			wantOut: "6 (B) 2026-07-10 file taxes +home due:2026-07-01\n",
		},
		{
			name:    "context case sensitive no match",
			args:    []string{"--context", "Phone"},
			wantOut: "",
		},
		{
			name:    "project exact token not substring",
			args:    []string{"--project", "hom"},
			wantOut: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, out, _ := runListCase(t, file, c.args...)
			if code != 0 {
				t.Fatalf("exit = %d, want 0", code)
			}
			if out != c.wantOut {
				t.Errorf("stdout = %q, want %q", out, c.wantOut)
			}
		})
	}
}

// TestListOverdueBoundary asserts the strict "before today" boundary: a task
// due exactly today is NOT overdue, one due yesterday is.
func TestListOverdueBoundary(t *testing.T) {
	file := seedList(t, ""+
		"2026-07-14 due today due:2026-07-14\n"+
		"2026-07-14 due yesterday due:2026-07-13\n"+
		"2026-07-14 no due date\n")
	code, out, errb := runListCase(t, file, "--overdue")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr %q)", code, errb)
	}
	if want := "2 2026-07-14 due yesterday due:2026-07-13\n"; out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
}

// TestListSortPri orders priority A→Z with unprioritized tasks after, and
// asserts the file line numbers stay attached to their tasks after reordering.
func TestListSortPri(t *testing.T) {
	// Line 1: (B), 2: unprioritized, 3: (A), 4: unprioritized, 5: (B).
	file := seedList(t, ""+
		"(B) 2026-07-14 beta one\n"+
		"2026-07-14 plain one\n"+
		"(A) 2026-07-14 alpha\n"+
		"2026-07-14 plain two\n"+
		"(B) 2026-07-14 beta two\n")
	code, out, errb := runListCase(t, file, "--sort", "pri")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr %q)", code, errb)
	}
	// A first (line 3), then the two B's in file order (1, 5), then the two
	// unprioritized in file order (2, 4). Numbers ride along with their tasks.
	want := "3 (A) 2026-07-14 alpha\n" +
		"1 (B) 2026-07-14 beta one\n" +
		"5 (B) 2026-07-14 beta two\n" +
		"2 2026-07-14 plain one\n" +
		"4 2026-07-14 plain two\n"
	if out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
}

// TestListSortDue orders earliest due first with no-due: tasks last, ties and
// the no-value group keeping file order.
func TestListSortDue(t *testing.T) {
	file := seedList(t, ""+
		"2026-07-14 later due:2026-07-20\n"+
		"2026-07-14 no due a\n"+
		"2026-07-14 earlier due:2026-07-10\n"+
		"2026-07-14 same as first due:2026-07-20\n"+
		"2026-07-14 no due b\n")
	code, out, errb := runListCase(t, file, "--sort", "due")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr %q)", code, errb)
	}
	// earliest (line 3), then the two due:2026-07-20 in file order (1, 4), then
	// the no-due group in file order (2, 5).
	want := "3 2026-07-14 earlier due:2026-07-10\n" +
		"1 2026-07-14 later due:2026-07-20\n" +
		"4 2026-07-14 same as first due:2026-07-20\n" +
		"2 2026-07-14 no due a\n" +
		"5 2026-07-14 no due b\n"
	if out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
}

// TestListSortCreated orders earliest creation date first with no-creation-date
// tasks last, ties and the no-value group keeping file order. Completed tasks
// without a creation date (only a completion date) fall into the no-value group.
func TestListSortCreated(t *testing.T) {
	file := seedList(t, ""+
		"2026-07-20 latest created\n"+
		"call with no creation date\n"+
		"2026-07-10 earliest created\n"+
		"2026-07-20 tie latest created\n")
	code, out, errb := runListCase(t, file, "--sort", "created")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr %q)", code, errb)
	}
	// earliest (line 3), then the two 2026-07-20 in file order (1, 4), then the
	// no-creation-date task (2) last.
	want := "3 2026-07-10 earliest created\n" +
		"1 2026-07-20 latest created\n" +
		"4 2026-07-20 tie latest created\n" +
		"2 call with no creation date\n"
	if out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
}

// TestListSortStableTies asserts a pure-tie sort (all same priority) is a no-op
// in order: the output is exactly file order with numbers intact.
func TestListSortStableTies(t *testing.T) {
	file := seedList(t, ""+
		"(A) 2026-07-14 first\n"+
		"(A) 2026-07-14 second\n"+
		"(A) 2026-07-14 third\n")
	code, out, errb := runListCase(t, file, "--sort", "pri")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr %q)", code, errb)
	}
	want := "1 (A) 2026-07-14 first\n" +
		"2 (A) 2026-07-14 second\n" +
		"3 (A) 2026-07-14 third\n"
	if out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
}

// TestListInvalidSort asserts an unrecognized --sort value is a usage error.
func TestListInvalidSort(t *testing.T) {
	file := seedList(t, listFixture)
	code, out, errb := runListCase(t, file, "--sort", "bogus")
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if !bytes.HasPrefix([]byte(errb), []byte("taskq: ")) {
		t.Errorf("stderr = %q, want taskq: prefix", errb)
	}
}

// TestListUnknownFlag asserts an unknown flag is a usage error (exit 1).
func TestListUnknownFlag(t *testing.T) {
	file := seedList(t, listFixture)
	code, _, errb := runListCase(t, file, "--nope")
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !bytes.HasPrefix([]byte(errb), []byte("taskq: ")) {
		t.Errorf("stderr = %q, want taskq: prefix", errb)
	}
}

// TestListUnexpectedArg asserts a stray positional argument is a usage error.
func TestListUnexpectedArg(t *testing.T) {
	file := seedList(t, listFixture)
	code, _, errb := runListCase(t, file, "extra")
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !bytes.HasPrefix([]byte(errb), []byte("taskq: ")) {
		t.Errorf("stderr = %q, want taskq: prefix", errb)
	}
}

// TestListFilterThenSort asserts filters and a sort compose: the survivors of a
// project filter are reordered by due date with their file numbers intact.
func TestListFilterThenSort(t *testing.T) {
	file := seedList(t, listFixture)
	code, out, errb := runListCase(t, file, "--project", "home", "--sort", "due")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr %q)", code, errb)
	}
	// Both +home tasks survive; line 6 has a due date (earliest), line 4 has none.
	want := "6 (B) 2026-07-10 file taxes +home due:2026-07-01\n" +
		"4 2026-07-14 buy milk +home @store\n"
	if out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
}
