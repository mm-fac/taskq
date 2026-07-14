package main

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// noEnv is a getenv stub that reports no environment variables set, keeping
// file-selection tests hermetic.
func noEnv(string) string { return "" }

// envMap returns a getenv stub backed by m.
func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// registerTest installs a throwaway command into the dispatch table for the
// duration of one test, asserting the name is free and removing it afterward.
// It demonstrates the registration point later subcommand items use.
func registerTest(t *testing.T, cmd command) {
	t.Helper()
	if _, exists := commands[cmd.name]; exists {
		t.Fatalf("test command %q already registered", cmd.name)
	}
	commands[cmd.name] = cmd
	t.Cleanup(func() { delete(commands, cmd.name) })
}

// TestRunUnknownCommand covers the dispatch table's trivial wiring: an
// unregistered command name is a usage error (exit 1) with a taskq:-prefixed
// diagnostic and no stdout.
func TestRunUnknownCommand(t *testing.T) {
	var out, errb bytes.Buffer
	code := run([]string{"frobnicate"}, &out, &errb, noEnv)
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.HasPrefix(errb.String(), "taskq: ") {
		t.Errorf("stderr = %q, want taskq: prefix", errb.String())
	}
	if !strings.Contains(errb.String(), "unknown command") {
		t.Errorf("stderr = %q, want to mention unknown command", errb.String())
	}
	if out.Len() != 0 {
		t.Errorf("stdout = %q, want empty", out.String())
	}
}

// TestDispatchRegisteredCommand shows a registered command is dispatched: its
// output reaches stdout and a nil return is exit 0 with clean stderr.
func TestDispatchRegisteredCommand(t *testing.T) {
	registerTest(t, command{name: "echo-ok", run: func(ctx *cmdContext) error {
		fmt.Fprintln(ctx.stdout, "ran")
		return nil
	}})
	var out, errb bytes.Buffer
	code := run([]string{"echo-ok"}, &out, &errb, noEnv)
	if code != 0 {
		t.Errorf("exit = %d, want 0 (stderr %q)", code, errb.String())
	}
	if out.String() != "ran\n" {
		t.Errorf("stdout = %q, want %q", out.String(), "ran\n")
	}
	if errb.Len() != 0 {
		t.Errorf("stderr = %q, want empty", errb.String())
	}
}

// TestRunUnknownGlobalFlag asserts an undefined global flag is a usage error.
func TestRunUnknownGlobalFlag(t *testing.T) {
	var out, errb bytes.Buffer
	code := run([]string{"--nope", "list"}, &out, &errb, noEnv)
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.HasPrefix(errb.String(), "taskq: ") {
		t.Errorf("stderr = %q, want taskq: prefix", errb.String())
	}
	if out.Len() != 0 {
		t.Errorf("stdout = %q, want empty", out.String())
	}
}

// TestRunNoCommand asserts an invocation with no subcommand is a usage error.
func TestRunNoCommand(t *testing.T) {
	var out, errb bytes.Buffer
	code := run(nil, &out, &errb, noEnv)
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.HasPrefix(errb.String(), "taskq: ") {
		t.Errorf("stderr = %q, want taskq: prefix", errb.String())
	}
}

// TestTodayInjection is the hermetic --today test: a pinned --today reaches the
// command verbatim as both the canonical string and the equivalent time.Time.
func TestTodayInjection(t *testing.T) {
	var gotStr string
	var gotTime time.Time
	registerTest(t, command{name: "show-today", run: func(ctx *cmdContext) error {
		gotStr = ctx.todayStr
		gotTime = ctx.today
		fmt.Fprintln(ctx.stdout, ctx.todayStr)
		return nil
	}})
	var out, errb bytes.Buffer
	code := run([]string{"--today", "2026-07-14", "show-today"}, &out, &errb, noEnv)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr %q)", code, errb.String())
	}
	if gotStr != "2026-07-14" {
		t.Errorf("todayStr = %q, want 2026-07-14", gotStr)
	}
	if want := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC); !gotTime.Equal(want) {
		t.Errorf("today = %v, want %v", gotTime, want)
	}
	if out.String() != "2026-07-14\n" {
		t.Errorf("stdout = %q, want %q", out.String(), "2026-07-14\n")
	}
}

// TestTodayInvalid covers the usage-error path: a malformed, non-zero-padded,
// calendar-invalid, or empty --today all exit 1 before any command runs.
func TestTodayInvalid(t *testing.T) {
	registerTest(t, command{name: "never", run: func(ctx *cmdContext) error {
		t.Error("command ran despite invalid --today")
		return nil
	}})
	for _, bad := range []string{"2026-7-14", "2026-13-01", "2026-02-30", "nope", ""} {
		var out, errb bytes.Buffer
		code := run([]string{"--today", bad, "never"}, &out, &errb, noEnv)
		if code != 1 {
			t.Errorf("--today %q: exit = %d, want 1", bad, code)
		}
		if !strings.HasPrefix(errb.String(), "taskq: ") {
			t.Errorf("--today %q: stderr = %q, want taskq: prefix", bad, errb.String())
		}
	}
}

// TestTodayDefaultsToSystemDate checks that, absent --today, the resolved today
// is a valid calendar date. It deliberately avoids asserting the exact value to
// stay independent of the wall clock.
func TestTodayDefaultsToSystemDate(t *testing.T) {
	var got string
	registerTest(t, command{name: "show-default-today", run: func(ctx *cmdContext) error {
		got = ctx.todayStr
		return nil
	}})
	var out, errb bytes.Buffer
	if code := run([]string{"show-default-today"}, &out, &errb, noEnv); code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr %q)", code, errb.String())
	}
	if !validDate(got) {
		t.Errorf("default today = %q, want a valid YYYY-MM-DD date", got)
	}
}

// TestGlobalFlagAfterSubcommandGoesToArgs asserts global flags are only honored
// before the subcommand: tokens after the command name are passed through to
// the command's args untouched, and the file path keeps its default.
func TestGlobalFlagAfterSubcommandGoesToArgs(t *testing.T) {
	var gotFile string
	var gotArgs []string
	registerTest(t, command{name: "capture-args", run: func(ctx *cmdContext) error {
		gotFile = ctx.filePath
		gotArgs = ctx.args
		return nil
	}})
	var out, errb bytes.Buffer
	code := run([]string{"capture-args", "--file", "other.txt"}, &out, &errb, noEnv)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr %q)", code, errb.String())
	}
	if gotFile != defaultFile {
		t.Errorf("filePath = %q, want default %q (--file after the command must not apply)", gotFile, defaultFile)
	}
	if want := []string{"--file", "other.txt"}; !reflect.DeepEqual(gotArgs, want) {
		t.Errorf("args = %q, want %q", gotArgs, want)
	}
}

// TestFileSelectionThroughRun checks the flag-wins-over-env precedence end to
// end, and that the done.txt path is derived from the chosen file's directory.
func TestFileSelectionThroughRun(t *testing.T) {
	var gotFile, gotDone string
	registerTest(t, command{name: "capture-file", run: func(ctx *cmdContext) error {
		gotFile, gotDone = ctx.filePath, ctx.donePath
		return nil
	}})

	env := envMap(map[string]string{"TASKQ_FILE": "/env/dir/env.txt"})

	// Flag wins over the environment variable.
	if code := run([]string{"--file", "/flag/dir/flag.txt", "capture-file"}, &bytes.Buffer{}, &bytes.Buffer{}, env); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if gotFile != "/flag/dir/flag.txt" {
		t.Errorf("filePath = %q, want the --file value", gotFile)
	}
	if gotDone != "/flag/dir/done.txt" {
		t.Errorf("donePath = %q, want /flag/dir/done.txt", gotDone)
	}

	// Without the flag, TASKQ_FILE is used.
	if code := run([]string{"capture-file"}, &bytes.Buffer{}, &bytes.Buffer{}, env); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if gotFile != "/env/dir/env.txt" {
		t.Errorf("filePath = %q, want the TASKQ_FILE value", gotFile)
	}
	if gotDone != "/env/dir/done.txt" {
		t.Errorf("donePath = %q, want /env/dir/done.txt", gotDone)
	}
}

// TestResolveFile unit-tests the selection precedence flag > env > default.
func TestResolveFile(t *testing.T) {
	env := envMap(map[string]string{"TASKQ_FILE": "env.txt"})
	if got := resolveFile("flag.txt", env); got != "flag.txt" {
		t.Errorf("with flag: got %q, want flag.txt", got)
	}
	if got := resolveFile("", env); got != "env.txt" {
		t.Errorf("with env: got %q, want env.txt", got)
	}
	if got := resolveFile("", noEnv); got != defaultFile {
		t.Errorf("with neither: got %q, want %q", got, defaultFile)
	}
}

// TestDeriveDonePath covers done.txt derivation for absolute paths, a
// non-default task-file name, and the bare/relative default.
func TestDeriveDonePath(t *testing.T) {
	cases := []struct{ file, want string }{
		{"/a/b/todo.txt", "/a/b/done.txt"},
		{"/a/b/tasks.md", "/a/b/done.txt"},
		{"todo.txt", "done.txt"},
		{"./todo.txt", "done.txt"},
		{defaultFile, "done.txt"},
	}
	for _, c := range cases {
		if got := deriveDonePath(c.file); got != c.want {
			t.Errorf("deriveDonePath(%q) = %q, want %q", c.file, got, c.want)
		}
	}
}

// TestExitCodeMapping locks the exit-code classes down: IO/file-format errors
// from the store map to 2 (including when wrapped), and every usage-class error
// — plus a bare error — maps to 1; nil is success.
func TestExitCodeMapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, 0},
		{"NoFileError", &NoFileError{Path: "x"}, 2},
		{"IOError", &IOError{Op: "read", Path: "x", Err: errors.New("boom")}, 2},
		{"wrapped IOError", fmt.Errorf("ctx: %w", &IOError{Op: "rename onto", Path: "x", Err: errors.New("boom")}), 2},
		{"RangeError", &RangeError{Num: 5, Count: 2}, 1},
		{"MalformedTargetError", &MalformedTargetError{Num: 3}, 1},
		{"usageError", usagef("empty add"), 1},
		{"plain error", errors.New("something"), 1},
	}
	for _, c := range cases {
		if got := exitCode(c.err); got != c.want {
			t.Errorf("%s: exitCode = %d, want %d", c.name, got, c.want)
		}
	}
}

// TestStoreErrorMapsToExit2 wires a store I/O-class error through run: a
// number-addressing mutation against a missing file is a NoFileError, which the
// skeleton reports as exit 2 with a taskq:-prefixed diagnostic.
func TestStoreErrorMapsToExit2(t *testing.T) {
	registerTest(t, command{name: "mutate", run: func(ctx *cmdContext) error {
		_, _, err := LoadForMutation(ctx.filePath)
		return err
	}})
	missing := filepath.Join(t.TempDir(), "nope.txt")
	var out, errb bytes.Buffer
	code := run([]string{"--file", missing, "mutate"}, &out, &errb, noEnv)
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if !strings.HasPrefix(errb.String(), "taskq: ") {
		t.Errorf("stderr = %q, want taskq: prefix", errb.String())
	}
}

// TestStoreUsageErrorMapsToExit1 wires a store usage-class error through run: a
// Resolve out-of-range against the committed fixture is a RangeError, exit 1.
func TestStoreUsageErrorMapsToExit1(t *testing.T) {
	registerTest(t, command{name: "resolve-bad", run: func(ctx *cmdContext) error {
		tasks, _, err := Load(ctx.filePath)
		if err != nil {
			return err
		}
		_, err = Resolve(tasks, 99)
		return err
	}})
	var out, errb bytes.Buffer
	code := run([]string{"--file", mixedFixture, "resolve-bad"}, &out, &errb, noEnv)
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.HasPrefix(errb.String(), "taskq: ") {
		t.Errorf("stderr = %q, want taskq: prefix", errb.String())
	}
}

// TestMalformedNoteThroughRun asserts the once-per-command note is emitted on
// stderr with the exact wording when the loaded file has malformed lines, and
// only once even if noteMalformed is called again.
func TestMalformedNoteThroughRun(t *testing.T) {
	registerTest(t, command{name: "load-note", run: func(ctx *cmdContext) error {
		_, n, err := Load(ctx.filePath)
		if err != nil {
			return err
		}
		ctx.noteMalformed(n)
		ctx.noteMalformed(n) // must not print a second time
		return nil
	}})
	var out, errb bytes.Buffer
	code := run([]string{"--file", mixedFixture, "load-note"}, &out, &errb, noEnv)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr %q)", code, errb.String())
	}
	if want := "taskq: skipped 2 malformed line(s)\n"; errb.String() != want {
		t.Errorf("stderr = %q, want exactly %q", errb.String(), want)
	}
}

// TestNoteMalformed unit-tests the note helper directly: no-op at zero, exact
// wording above zero, and at most once.
func TestNoteMalformed(t *testing.T) {
	var errb bytes.Buffer
	ctx := &cmdContext{stderr: &errb}

	ctx.noteMalformed(0)
	if errb.Len() != 0 {
		t.Errorf("n=0 wrote %q, want nothing", errb.String())
	}

	ctx.noteMalformed(3)
	if want := "taskq: skipped 3 malformed line(s)\n"; errb.String() != want {
		t.Errorf("n=3 wrote %q, want %q", errb.String(), want)
	}

	ctx.noteMalformed(3) // once-per-command: no additional output
	if want := "taskq: skipped 3 malformed line(s)\n"; errb.String() != want {
		t.Errorf("second call changed stderr to %q", errb.String())
	}
}

// TestStdoutStderrSplit confirms the stream contract: command output goes to
// stdout while the diagnostic for a returned error goes to stderr, prefixed.
func TestStdoutStderrSplit(t *testing.T) {
	registerTest(t, command{name: "split", run: func(ctx *cmdContext) error {
		fmt.Fprintln(ctx.stdout, "listing line")
		return usagef("bad request")
	}})
	var out, errb bytes.Buffer
	code := run([]string{"split"}, &out, &errb, noEnv)
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if out.String() != "listing line\n" {
		t.Errorf("stdout = %q, want %q", out.String(), "listing line\n")
	}
	if !strings.HasPrefix(errb.String(), "taskq: ") {
		t.Errorf("stderr = %q, want taskq: prefix", errb.String())
	}
}

// TestReportErrorPrefix asserts the skeleton adds the taskq: prefix to a bare
// error and leaves an already-prefixed store error intact, returning the class.
func TestReportErrorPrefix(t *testing.T) {
	var bare bytes.Buffer
	if code := reportError(&bare, errors.New("boom")); code != 1 {
		t.Errorf("bare error code = %d, want 1", code)
	}
	if bare.String() != "taskq: boom\n" {
		t.Errorf("bare error stderr = %q, want %q", bare.String(), "taskq: boom\n")
	}

	var io bytes.Buffer
	ioErr := &IOError{Op: "read", Path: "p", Err: errors.New("x")}
	if code := reportError(&io, ioErr); code != 2 {
		t.Errorf("IOError code = %d, want 2", code)
	}
	if !strings.HasPrefix(io.String(), "taskq: ") {
		t.Errorf("IOError stderr = %q, want taskq: prefix", io.String())
	}
	if strings.HasPrefix(io.String(), "taskq: taskq: ") {
		t.Errorf("IOError stderr = %q, prefix was doubled", io.String())
	}
}

// TestHelpFlag asserts -h / --help before the command prints usage to stdout
// and exits 0.
func TestHelpFlag(t *testing.T) {
	for _, arg := range []string{"-h", "--help"} {
		var out, errb bytes.Buffer
		code := run([]string{arg}, &out, &errb, noEnv)
		if code != 0 {
			t.Errorf("%s: exit = %d, want 0", arg, code)
		}
		if !strings.Contains(out.String(), "Usage:") {
			t.Errorf("%s: stdout = %q, want usage on stdout", arg, out.String())
		}
		if errb.Len() != 0 {
			t.Errorf("%s: stderr = %q, want empty", arg, errb.String())
		}
	}
}
