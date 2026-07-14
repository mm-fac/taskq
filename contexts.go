package main

import "fmt"

// contexts.go implements `taskq contexts`: the @context twin of `projects`.
// Its scope is fixed to open tasks in the task file, and it performs no writes.

func init() {
	register(command{
		name:    "contexts",
		summary: "list distinct @context tokens in open tasks",
		run:     runContexts,
	})
}

// runContexts loads the task file and prints its distinct open-task context
// tokens in byte order. A missing file is an empty result, and arguments after
// the command are rejected because contexts has no command-specific flags.
func runContexts(ctx *cmdContext) error {
	if len(ctx.args) > 0 {
		return usagef("contexts: unexpected argument %q", ctx.args[0])
	}

	tasks, malformed, err := Load(ctx.filePath)
	if err != nil {
		return err
	}
	ctx.noteMalformed(malformed)

	for _, tok := range distinctSigilTokens(tasks, '@') {
		fmt.Fprintln(ctx.stdout, tok)
	}
	return nil
}
