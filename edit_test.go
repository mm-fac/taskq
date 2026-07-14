package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const editFixture = "testdata/edit.txt"

func editFile(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "todo.txt")
}

func readEditFixture(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(editFixture)
	if err != nil {
		t.Fatalf("read edit fixture: %v", err)
	}
	return string(data)
}

func seedEditFixture(t *testing.T, file string) string {
	t.Helper()
	seed := readEditFixture(t)
	seedFile(t, file, seed)
	return seed
}

func runEditCase(t *testing.T, file string, args ...string) (int, string, string) {
	t.Helper()
	full := append([]string{"--today", "2026-07-15", "--file", file, "edit"}, args...)
	var out, errb bytes.Buffer
	code := run(full, &out, &errb, noEnv)
	return code, out.String(), errb.String()
}

// TestEditReplacesOnlyText is table-driven over the committed fixture. It
// covers argument joining, line-number output, preservation of priority and
// creation date (or their absence), and verbatim storage of project, context,
// and due-looking tokens.
func TestEditReplacesOnlyText(t *testing.T) {
	cases := []struct {
		name    string
		num     string
		args    []string
		oldLine string
		newLine string
	}{
		{
			name:    "priority and creation date preserved",
			num:     "1",
			args:    []string{"call", "the", "dentist"},
			oldLine: "(A) 2026-07-14 call the bank +errands @phone due:2026-07-20",
			newLine: "(A) 2026-07-14 call the dentist",
		},
		{
			name:    "tokens stored verbatim",
			num:     "2",
			args:    []string{"ship", "+Release-1", "@Ops", "due:not-a-date", "due:2026-99-99"},
			oldLine: "2026-07-10 buy milk",
			newLine: "2026-07-10 ship +Release-1 @Ops due:not-a-date due:2026-99-99",
		},
		{
			name:    "creation date and priority remain absent",
			num:     "3",
			args:    []string{"mist", "the", "ferns"},
			oldLine: "water plants",
			newLine: "mist the ferns",
		},
		{
			name:    "priority preserved without creation date",
			num:     "7",
			args:    []string{"submit", "return"},
			oldLine: "(Z) file taxes",
			newLine: "(Z) submit return",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file := editFile(t)
			seed := seedEditFixture(t, file)
			code, out, errb := runEditCase(t, file, append([]string{tc.num}, tc.args...)...)
			if code != 0 {
				t.Fatalf("exit = %d, want 0 (stderr %q)", code, errb)
			}
			if want := tc.num + " " + tc.newLine + "\n"; out != want {
				t.Errorf("stdout = %q, want %q", out, want)
			}
			if errb != "taskq: skipped 2 malformed line(s)\n" {
				t.Errorf("stderr = %q, want malformed-lines note", errb)
			}
			wantFile := strings.Replace(seed, tc.oldLine, tc.newLine, 1)
			if got := readFile(t, file); got != wantFile {
				t.Errorf("file = %q, want %q", got, wantFile)
			}
		})
	}
}

// TestEditUsageErrors verifies every rejected invocation leaves the committed
// fixture byte-identical. This includes empty text, bad numbers, a completed
// target, and both malformed lines addressed by their editor-visible numbers.
func TestEditUsageErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{name: "no arguments"},
		{name: "number only", args: []string{"1"}},
		{name: "empty argument", args: []string{"1", ""}},
		{name: "whitespace only", args: []string{"1", " \t "}},
		{name: "non-numeric number", args: []string{"nope", "text"}},
		{name: "zero number", args: []string{"0", "text"}},
		{name: "out of range", args: []string{"8", "text"}},
		{name: "completed target", args: []string{"4", "new", "text"}},
		{name: "blank malformed target", args: []string{"5", "new", "text"}},
		{name: "second blank malformed target", args: []string{"6", "new", "text"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file := editFile(t)
			seed := seedEditFixture(t, file)
			code, out, errb := runEditCase(t, file, tc.args...)
			if code != 1 {
				t.Errorf("exit = %d, want 1 (stderr %q)", code, errb)
			}
			if out != "" {
				t.Errorf("stdout = %q, want empty", out)
			}
			if !strings.HasPrefix(errb, "taskq: ") {
				t.Errorf("stderr = %q, want taskq: prefix", errb)
			}
			if got := readFile(t, file); got != seed {
				t.Errorf("file changed on usage error:\n got  %q\n want %q", got, seed)
			}
		})
	}
}

// TestEditPriorityPrefixRejected checks the exact uppercase `(A) `-`(Z) `
// rejection, including joined and single arguments. Its diagnostic directs
// users to pri, while similar-looking text remains valid ordinary text.
func TestEditPriorityPrefixRejected(t *testing.T) {
	rejected := []struct {
		name string
		args []string
	}{
		{name: "A joined from arguments", args: []string{"1", "(A)", "new", "text"}},
		{name: "Z in one argument", args: []string{"1", "(Z) new text"}},
		{name: "prefix without following text", args: []string{"1", "(B) "}},
	}
	for _, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			file := editFile(t)
			seed := seedEditFixture(t, file)
			code, out, errb := runEditCase(t, file, tc.args...)
			if code != 1 {
				t.Errorf("exit = %d, want 1 (stderr %q)", code, errb)
			}
			if out != "" {
				t.Errorf("stdout = %q, want empty", out)
			}
			if !strings.Contains(errb, "pri") {
				t.Errorf("stderr = %q, want message pointing at pri", errb)
			}
			if got := readFile(t, file); got != seed {
				t.Errorf("file changed on priority-prefix error")
			}
		})
	}

	accepted := []struct {
		name string
		text string
	}{
		{name: "lowercase priority lookalike", text: "(a) ordinary text"},
		{name: "no separating space", text: "(A)ordinary text"},
		{name: "multi-letter parens", text: "(AB) ordinary text"},
		{name: "bare priority token", text: "(A)"},
	}
	for _, tc := range accepted {
		t.Run(tc.name, func(t *testing.T) {
			file := editFile(t)
			seedEditFixture(t, file)
			code, out, errb := runEditCase(t, file, "2", tc.text)
			if code != 0 {
				t.Fatalf("exit = %d, want 0 (stderr %q)", code, errb)
			}
			if want := "2 2026-07-10 " + tc.text + "\n"; out != want {
				t.Errorf("stdout = %q, want %q", out, want)
			}
		})
	}
}

func TestEditMissingFileIsIOError(t *testing.T) {
	file := editFile(t)
	code, out, errb := runEditCase(t, file, "1", "new", "text")
	if code != 2 {
		t.Errorf("exit = %d, want 2 (stderr %q)", code, errb)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if !strings.HasPrefix(errb, "taskq: ") {
		t.Errorf("stderr = %q, want taskq: prefix", errb)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("missing task file was created, stat error = %v", err)
	}
}

// TestEditAtomicRewriteAndMalformedPreservation starts from the committed
// fixture without its final newline. A successful edit must preserve both
// malformed lines byte-for-byte, normalize the result to exactly one trailing
// newline, and leave no temporary file beside the task file.
func TestEditAtomicRewriteAndMalformedPreservation(t *testing.T) {
	file := editFile(t)
	seed := strings.TrimSuffix(readEditFixture(t), "\n")
	seedFile(t, file, seed)

	code, out, errb := runEditCase(t, file, "7", "send", "forms")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr %q)", code, errb)
	}
	if out != "7 (Z) send forms\n" {
		t.Errorf("stdout = %q, want resulting line", out)
	}
	if errb != "taskq: skipped 2 malformed line(s)\n" {
		t.Errorf("stderr = %q, want malformed-lines note", errb)
	}

	want := strings.Replace(seed, "(Z) file taxes", "(Z) send forms", 1) + "\n"
	got := readFile(t, file)
	if got != want {
		t.Errorf("file = %q, want %q", got, want)
	}
	if !strings.HasSuffix(got, "\n") || strings.HasSuffix(got, "\n\n") {
		t.Errorf("file does not end with exactly one newline: %q", got)
	}

	entries, err := os.ReadDir(filepath.Dir(file))
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "todo.txt" {
		names := make([]string, len(entries))
		for i, entry := range entries {
			names[i] = entry.Name()
		}
		t.Errorf("directory entries = %v, want exactly [todo.txt]", names)
	}
}
