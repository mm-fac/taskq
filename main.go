// Command taskq is a plain-text task tracker. See requirements.md for the
// v0.1 specification; the work-item graph builds it out from this stub.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "taskq: no command given (see requirements.md)")
	os.Exit(1)
}
