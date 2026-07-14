package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// projects_test.go covers `taskq projects`: distinct `+project` tokens from the
// OPEN tasks of the task file, one per line, sigil retained, deduplicated, and
// sorted by byte order. Every case pins --today to stay hermetic even though
// the command reads no dates. Fixtures are committed (testdata/projects.txt)
// and, for the focused table cases, seeded inline verbatim.

const projectsFixture = "testdata/projects.txt"

// runProjectsCase invokes the real dispatch (run) for `projects` against file
// with a pinned --today, returning the exit code and captured streams. Going
// through run exercises the registered command and global-flag parsing.
func runProjectsCase(t *testing.T, file string, args ...string) (int, string, string) {
	t.Helper()
	full := append([]string{"--today", "2026-07-14", "--file", file, "projects"}, args...)
	var out, errb bytes.Buffer
	code := run(full, &out, &errb, noEnv)
	return code, out.String(), errb.String()
}

// TestProjectsFromFixture reads the committed fixture and asserts the exact,
// byte-order-sorted, deduplicated listing. The fixture exercises dedup (+Work
// and +errands each appear in two open tasks), byte-order sort (uppercase +Work
// sorts before the lowercase-initial tokens), sigil retention, the bare-sigil
// rule (a lone "+" is not a token), and open-task scope (the completed task's
// +ghost and the malformed lines contribute nothing).
func TestProjectsFromFixture(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	data, err := os.ReadFile(projectsFixture)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	code, out, errb := runProjectsCase(t, file)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr %q)", code, errb)
	}
	// The fixture's two malformed lines (blank, spaces) draw the standard
	// once-per-command note on stderr; the listing itself is unaffected.
	if wantErr := "taskq: skipped 2 malformed line(s)\n"; errb != wantErr {
		t.Errorf("stderr = %q, want %q", errb, wantErr)
	}
	want := "+Work\n+admin\n+errands\n+home\n"
	if out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
}

// TestProjects is table-driven over inline-seeded task files, isolating each
// acceptance behaviour: dedup, byte-order sort, sigil retention, open-task
// scope (completed and malformed excluded), the bare-sigil rule, and an empty
// result. Contents are written verbatim so the exact bytes under test are pinned.
func TestProjects(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "dedup across open tasks",
			content: "2026-07-14 a +home\n2026-07-14 b +home\n2026-07-14 c +home\n",
			want:    "+home\n",
		},
		{
			name:    "byte-order sort puts uppercase before lowercase",
			content: "2026-07-14 a +banana +Apple +cherry\n",
			want:    "+Apple\n+banana\n+cherry\n",
		},
		{
			name:    "sigil retained and multiple tokens per line",
			content: "2026-07-14 pay +bills +rent\n",
			want:    "+bills\n+rent\n",
		},
		{
			name:    "completed tasks excluded",
			content: "2026-07-14 open +keep\nx 2026-07-15 2026-07-14 done +drop\n",
			want:    "+keep\n",
		},
		{
			name:    "malformed lines excluded",
			content: "\n   \n2026-07-14 well-formed task +ok\n",
			want:    "+ok\n",
		},
		{
			name:    "bare sigil is not a token",
			content: "2026-07-14 a + +real\n",
			want:    "+real\n",
		},
		{
			name:    "priority and creation-date prefixes are not scanned",
			content: "(A) 2026-07-14 real +proj\n",
			want:    "+proj\n",
		},
		{
			name:    "no project tokens yields empty output",
			content: "2026-07-14 plain task @context due:2026-07-20\n",
			want:    "",
		},
		{
			name:    "project only in a completed task yields empty output",
			content: "x 2026-07-15 2026-07-14 done +only\n",
			want:    "",
		},
		{
			name:    "empty file yields empty output",
			content: "",
			want:    "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "todo.txt")
			if err := os.WriteFile(file, []byte(tc.content), 0o644); err != nil {
				t.Fatalf("seed write: %v", err)
			}
			code, out, errb := runProjectsCase(t, file)
			if code != 0 {
				t.Fatalf("exit = %d, want 0 (stderr %q)", code, errb)
			}
			if out != tc.want {
				t.Errorf("stdout = %q, want %q", out, tc.want)
			}
		})
	}
}

// TestProjectsMissingFile covers a missing task file: no output, exit 0
// (read-command convention), and — crucially — no file is created (the command
// performs no writes).
func TestProjectsMissingFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")

	code, out, errb := runProjectsCase(t, file)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr %q)", code, errb)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if errb != "" {
		t.Errorf("stderr = %q, want empty", errb)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("task file exists after read command; want no write (stat err %v)", err)
	}
}

// TestProjectsMalformedNote confirms the standard once-per-command malformed
// note is emitted on stderr while the listing itself is unaffected.
func TestProjectsMalformedNote(t *testing.T) {
	file := filepath.Join(t.TempDir(), "todo.txt")
	// Two malformed lines (blank, spaces) plus an open task with a project.
	if err := os.WriteFile(file, []byte("\n   \n2026-07-14 buy milk +home\n"), 0o644); err != nil {
		t.Fatalf("seed write: %v", err)
	}
	code, out, errb := runProjectsCase(t, file)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr %q)", code, errb)
	}
	if out != "+home\n" {
		t.Errorf("stdout = %q, want %q", out, "+home\n")
	}
	if want := "taskq: skipped 2 malformed line(s)\n"; errb != want {
		t.Errorf("stderr = %q, want %q", errb, want)
	}
}

// TestProjectsRejectsArgs confirms no positional arguments or flags beyond the
// globals are accepted: the scope is fixed to open tasks of the task file.
func TestProjectsRejectsArgs(t *testing.T) {
	file := filepath.Join(t.TempDir(), "todo.txt")
	if err := os.WriteFile(file, []byte("2026-07-14 a +home\n"), 0o644); err != nil {
		t.Fatalf("seed write: %v", err)
	}
	for _, arg := range []string{"extra", "--all", "--project"} {
		code, out, errb := runProjectsCase(t, file, arg)
		if code != 1 {
			t.Errorf("arg %q: exit = %d, want 1 (usage)", arg, code)
		}
		if out != "" {
			t.Errorf("arg %q: stdout = %q, want empty", arg, out)
		}
		if errb == "" {
			t.Errorf("arg %q: want a stderr diagnostic", arg)
		}
	}
}
