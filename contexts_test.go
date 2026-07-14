package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

const (
	contextsFixture      = "testdata/contexts.txt"
	contextsEmptyFixture = "testdata/contexts_empty.txt"
	contextsDoneFixture  = "testdata/contexts_done.txt"
)

// runContextsCase invokes the registered command through the real dispatcher,
// with today pinned so every case is hermetic.
func runContextsCase(t *testing.T, file string, args ...string) (int, string, string) {
	t.Helper()
	full := append([]string{"--today", "2026-07-14", "--file", file, "contexts"}, args...)
	var out, errb bytes.Buffer
	code := run(full, &out, &errb, noEnv)
	return code, out.String(), errb.String()
}

func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %q: %v", path, err)
	}
	return data
}

// TestContextsFromFixtures is table-driven over committed inputs. The main
// fixture covers exact sigil retention, deduplication, byte-order sorting,
// punctuation retained as part of a whitespace-delimited token, the bare-@
// rule, and exclusion of completed tasks. The empty fixture covers exit-zero
// empty output. A populated done.txt fixture proves the command never sources
// archived tasks. Comparing every input afterward also locks down read-only
// behavior.
func TestContextsFromFixtures(t *testing.T) {
	cases := []struct {
		name        string
		taskFixture string
		doneFixture string
		want        string
		wantErr     string
	}{
		{
			name:        "distinct open contexts",
			taskFixture: contextsFixture,
			doneFixture: contextsDoneFixture,
			want:        "@Work\n@admin\n@desk,\n@phone\n@store\n",
			wantErr:     "taskq: skipped 1 malformed line(s)\n",
		},
		{
			name:        "no open contexts",
			taskFixture: contextsEmptyFixture,
			doneFixture: contextsDoneFixture,
			want:        "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			taskFile := filepath.Join(dir, "todo.txt")
			doneFile := filepath.Join(dir, "done.txt")
			taskData := readFixture(t, tc.taskFixture)
			doneData := readFixture(t, tc.doneFixture)
			if err := os.WriteFile(taskFile, taskData, 0o644); err != nil {
				t.Fatalf("seed task file: %v", err)
			}
			if err := os.WriteFile(doneFile, doneData, 0o644); err != nil {
				t.Fatalf("seed done file: %v", err)
			}

			code, out, errb := runContextsCase(t, taskFile)
			if code != 0 {
				t.Fatalf("exit = %d, want 0 (stderr %q)", code, errb)
			}
			if out != tc.want {
				t.Errorf("stdout = %q, want %q", out, tc.want)
			}
			if errb != tc.wantErr {
				t.Errorf("stderr = %q, want %q", errb, tc.wantErr)
			}

			if got := readFixture(t, taskFile); !bytes.Equal(got, taskData) {
				t.Errorf("task file changed: got %q, want %q", got, taskData)
			}
			if got := readFixture(t, doneFile); !bytes.Equal(got, doneData) {
				t.Errorf("done file changed: got %q, want %q", got, doneData)
			}
		})
	}
}

// TestContextsMissingFile covers the read-command convention: a missing task
// file is empty and successful, and neither todo.txt nor done.txt is created.
func TestContextsMissingFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")

	code, out, errb := runContextsCase(t, file)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr %q)", code, errb)
	}
	if out != "" || errb != "" {
		t.Errorf("stdout, stderr = %q, %q; want both empty", out, errb)
	}
	for _, path := range []string{file, filepath.Join(dir, "done.txt")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s exists after contexts; want no write (stat err %v)", path, err)
		}
	}
}

// TestContextsRejectsArgs confirms contexts accepts no positional arguments or
// command-specific flags; global flags are accepted only before the command.
func TestContextsRejectsArgs(t *testing.T) {
	file := filepath.Join(t.TempDir(), "todo.txt")
	if err := os.WriteFile(file, []byte("2026-07-14 call @phone\n"), 0o644); err != nil {
		t.Fatalf("seed task file: %v", err)
	}

	for _, arg := range []string{"extra", "--all", "--context"} {
		code, out, errb := runContextsCase(t, file, arg)
		if code != 1 {
			t.Errorf("arg %q: exit = %d, want 1", arg, code)
		}
		if out != "" {
			t.Errorf("arg %q: stdout = %q, want empty", arg, out)
		}
		if errb == "" {
			t.Errorf("arg %q: want a stderr diagnostic", arg)
		}
	}
}
