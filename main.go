// Command taskq is a plain-text task tracker. See requirements.md for the
// v0.1 specification; the work-item graph builds it out from this stub.
package main

import "os"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, os.Getenv))
}
