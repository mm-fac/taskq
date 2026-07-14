package main

import (
	"fmt"
	"sort"
	"strings"
)

// projects.go implements `taskq projects`: it prints every distinct `+project`
// token found in the OPEN tasks of the task file, one per line, each retaining
// its `+` sigil, byte-exact as written, deduplicated, and sorted lexically by
// byte order. It is read-only — it uses the store's Load and never mutates or
// creates any file.
//
// Scope is fixed (requirements.md, the `projects`/`contexts` entry): only open
// tasks of the task file contribute. Completed tasks and malformed lines are
// ignored, done.txt is never read, and no flags beyond the globals are
// accepted. An empty result prints nothing (exit 0); a missing task file is an
// empty result too (read-command convention), with no write performed.
//
// The token extraction is factored into distinctSigilTokens, parameterised by
// the sigil byte, so `contexts` (item 4) can reuse it for `@context` tokens.

func init() {
	register(command{
		name:    "projects",
		summary: "list distinct +project tokens in open tasks",
		run:     runProjects,
	})
}

// runProjects loads the task file (a missing file is an empty result, not an
// error), scans the open tasks for `+project` tokens, and prints the distinct
// ones in byte order. It accepts no arguments or flags beyond the globals.
func runProjects(ctx *cmdContext) error {
	if len(ctx.args) > 0 {
		return usagef("projects: unexpected argument %q", ctx.args[0])
	}

	tasks, malformed, err := Load(ctx.filePath)
	if err != nil {
		return err
	}
	ctx.noteMalformed(malformed)

	for _, tok := range distinctSigilTokens(tasks, '+') {
		fmt.Fprintln(ctx.stdout, tok)
	}
	return nil
}

// distinctSigilTokens returns the distinct sigil-prefixed tokens found in the
// OPEN tasks of tasks, sorted lexically by byte order, each retaining its
// sigil byte-exactly as written. A token is a whitespace-delimited word of the
// task's text beginning with sigil and having at least one following character
// (a lone sigil is not a token). Completed tasks and malformed lines contribute
// nothing. It is parameterised by sigil so both `projects` ('+') and, later,
// `contexts` ('@') can share the open-task scan.
func distinctSigilTokens(tasks []Task, sigil byte) []string {
	seen := make(map[string]struct{})
	for _, t := range tasks {
		if t.Malformed || t.Completed {
			continue
		}
		for _, f := range strings.Fields(t.Text) {
			if len(f) >= 2 && f[0] == sigil {
				seen[f] = struct{}{}
			}
		}
	}

	tokens := make([]string, 0, len(seen))
	for tok := range seen {
		tokens = append(tokens, tok)
	}
	sort.Strings(tokens) // Go string order is byte order.
	return tokens
}
