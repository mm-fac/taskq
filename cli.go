package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// cli.go is the command-line skeleton. It parses the global flags that precede
// the subcommand, resolves the injected "today" and the task-file / done-file
// paths, dispatches to a registered subcommand, prints the once-per-command
// malformed-lines note, and maps command outcomes onto the process exit-code
// classes with `taskq: `-prefixed diagnostics. Concrete subcommands register
// themselves in the dispatch table (see register) and are implemented by later
// work items; this file does not change when a subcommand is added.

// defaultFile is the task-file path used when neither --file nor TASKQ_FILE
// selects one (requirements.md, "The task file").
const defaultFile = "./todo.txt"

// --- dispatch table ---------------------------------------------------------

// command is one registered subcommand. run receives a fully-resolved
// cmdContext (paths, today, args, output streams) and returns nil on success or
// a classified error (see exitCode) on failure.
type command struct {
	name    string
	summary string // one-line description for help; may be empty
	run     func(ctx *cmdContext) error
}

// commands is the dispatch table. Subcommand work items populate it by calling
// register (typically from an init function in their own file), so the dispatch
// logic in run never has to be rewritten to add a command.
var commands = map[string]command{}

// register adds cmd to the dispatch table. A later registration with the same
// name replaces an earlier one. It is intended to be called during package
// initialisation by each subcommand's file.
func register(cmd command) {
	commands[cmd.name] = cmd
}

// --- command context --------------------------------------------------------

// cmdContext carries everything a subcommand needs, resolved once by run: the
// task-file path, the derived done.txt path, the injected today (as both a
// time.Time and its canonical YYYY-MM-DD string), the arguments that followed
// the subcommand name, and the output streams. Diagnostics go to stderr (always
// `taskq: `-prefixed); listings and command output go to stdout.
type cmdContext struct {
	filePath string
	donePath string
	today    time.Time
	todayStr string
	args     []string
	stdout   io.Writer
	stderr   io.Writer

	noted bool // guards noteMalformed against printing more than once
}

// noteMalformed prints the standard `taskq: skipped N malformed line(s)` note
// to stderr when n > 0. It is a no-op for n <= 0 and prints at most once per
// command, so a subcommand may call it unconditionally after loading.
func (c *cmdContext) noteMalformed(n int) {
	if n <= 0 || c.noted {
		return
	}
	c.noted = true
	fmt.Fprintf(c.stderr, "taskq: skipped %d malformed line(s)\n", n)
}

// --- usage-class error ------------------------------------------------------

// usageError marks a usage-class failure (exit 1): a bad invocation the user
// can fix — unknown command or flag, a malformed argument, conflicting flags,
// an empty add. IO/file-format failures instead surface as the store's
// *IOError / *NoFileError (exit 2); see exitCode. Its message already carries
// the `taskq: ` prefix.
type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

// usagef builds a usageError with a `taskq: `-prefixed, printf-formatted
// message, for the skeleton and subcommands to report usage problems.
func usagef(format string, a ...any) error {
	return &usageError{msg: "taskq: " + fmt.Sprintf(format, a...)}
}

// --- run --------------------------------------------------------------------

// run is the testable entry point: it executes one invocation and returns the
// process exit code — 0 success, 1 usage error, 2 IO/file-format failure. args
// is the argument list after the program name; stdout/stderr are the output
// streams; getenv resolves environment variables (os.Getenv in main, a stub in
// tests) so file selection via TASKQ_FILE stays hermetic.
func run(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	fs := flag.NewFlagSet("taskq", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // we print our own taskq:-prefixed diagnostics
	var fileFlag, todayFlag string
	fs.StringVar(&fileFlag, "file", "", "task file path")
	fs.StringVar(&todayFlag, "today", "", "injected today as YYYY-MM-DD")

	// Global flags are only accepted before the subcommand: flag.Parse stops at
	// the first non-flag token, so anything after the command name is left for
	// the subcommand's own parsing (Go flag convention, requirements.md).
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage(stdout)
			return 0
		}
		fmt.Fprintf(stderr, "taskq: %v\n", err)
		return 1
	}

	// Resolve the injected today. Distinguish "flag absent" from "flag set to an
	// empty value" via Visit, so --today "" is a usage error rather than a silent
	// fall-through to the system date.
	todaySet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "today" {
			todaySet = true
		}
	})
	today, todayStr, err := resolveToday(todayFlag, todaySet)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	rest := fs.Args()
	if len(rest) == 0 {
		fmt.Fprintln(stderr, "taskq: no command given")
		return 1
	}
	name, cmdArgs := rest[0], rest[1:]
	cmd, ok := commands[name]
	if !ok {
		fmt.Fprintf(stderr, "taskq: unknown command %q\n", name)
		return 1
	}

	filePath := resolveFile(fileFlag, getenv)
	ctx := &cmdContext{
		filePath: filePath,
		donePath: deriveDonePath(filePath),
		today:    today,
		todayStr: todayStr,
		args:     cmdArgs,
		stdout:   stdout,
		stderr:   stderr,
	}
	if err := cmd.run(ctx); err != nil {
		return reportError(stderr, err)
	}
	return 0
}

// --- resolution helpers -----------------------------------------------------

// resolveToday returns the injected today as a time.Time and its canonical
// YYYY-MM-DD string. When --today was set it must be a zero-padded,
// calendar-valid date (validDate, shared with the model) or it is a usage
// error; when it was not set the system date in local time is used, which is
// why tests must pin --today to stay hermetic (requirements.md, "Today,
// injectable").
func resolveToday(value string, set bool) (time.Time, string, error) {
	if set {
		if !validDate(value) {
			return time.Time{}, "", usagef("invalid --today %q: want YYYY-MM-DD", value)
		}
		t, _ := time.Parse("2006-01-02", value) // validDate guarantees success
		return t, value, nil
	}
	// Absent --today, use the system date in local time, but normalise it to the
	// same date-only, UTC-midnight time.Time the flag path yields, so downstream
	// date comparisons never see a time-of-day or zone offset.
	s := time.Now().Format("2006-01-02")
	t, _ := time.Parse("2006-01-02", s)
	return t, s, nil
}

// resolveFile selects the task-file path: the --file flag wins, then the
// TASKQ_FILE environment variable, then the default ./todo.txt.
func resolveFile(fileFlag string, getenv func(string) string) string {
	if fileFlag != "" {
		return fileFlag
	}
	if env := getenv("TASKQ_FILE"); env != "" {
		return env
	}
	return defaultFile
}

// deriveDonePath returns the archive target: done.txt in the same directory as
// the task file, whatever the task file is named (requirements.md, "The task
// file"). Archiving itself is a later work item; only the derivation lives here.
func deriveDonePath(filePath string) string {
	return filepath.Join(filepath.Dir(filePath), "done.txt")
}

// --- error reporting --------------------------------------------------------

// reportError prints err to stderr with the mandatory `taskq: ` prefix and
// returns its exit code. The prefix is enforced here so every diagnostic
// satisfies the contract regardless of how a subcommand built the error.
func reportError(stderr io.Writer, err error) int {
	msg := err.Error()
	if !strings.HasPrefix(msg, "taskq: ") {
		msg = "taskq: " + msg
	}
	fmt.Fprintln(stderr, msg)
	return exitCode(err)
}

// exitCode maps an error to its process exit-code class: 2 for I/O or
// file-format failures — the store's *IOError and *NoFileError (unreadable or
// unwritable files, a missing file for a number-addressing mutation, rename
// failure) — and 1 for every other error, which is a usage problem (bad task
// number, malformed target, invalid date, empty add, conflicting or unknown
// flags). A nil error is success (0).
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var nfe *NoFileError
	var ioe *IOError
	if errors.As(err, &nfe) || errors.As(err, &ioe) {
		return 2
	}
	return 1
}

// --- help -------------------------------------------------------------------

// printUsage writes a short top-level usage summary, including any registered
// subcommands, to w. Per-command help is added by later work items.
func printUsage(w io.Writer) {
	fmt.Fprintln(w, "taskq — a plain-text task tracker")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  taskq [--file <path>] [--today YYYY-MM-DD] <command> [args...]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Global flags (before the command):")
	fmt.Fprintf(w, "  --file <path>       task file (default %s, or $TASKQ_FILE)\n", defaultFile)
	fmt.Fprintln(w, `  --today YYYY-MM-DD   injected "today" (default: system local date)`)

	if len(commands) == 0 {
		return
	}
	names := make([]string, 0, len(commands))
	for n := range commands {
		names = append(names, n)
	}
	sort.Strings(names)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	for _, n := range names {
		if s := commands[n].summary; s != "" {
			fmt.Fprintf(w, "  %-9s %s\n", n, s)
		} else {
			fmt.Fprintf(w, "  %s\n", n)
		}
	}
}
