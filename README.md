# taskq

`taskq` is a small command-line task tracker over a plain-text file (one task
per line, todo.txt-inspired). All state lives in the task file; there is no
database, no daemon, no network, and no dependency beyond the Go standard
library.

See [`requirements.md`](requirements.md) for the v0.1 specification.

## Build

```
go build ./...
go test ./...
```
