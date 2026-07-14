# taskq — requirements (v0.2)

> Owner-signed 2026-07-14 (M4 study input; sign-off recorded in the ops
> DECISION-LOG). This document is owner-owned: only owner-signed revisions
> may change it, landed via operator PR. It is the input to the automated
> Program layer (v8 §3.4).

**Product:** `taskq`, a command-line task tracker over a plain-text file.
One task per line in a todo.txt-inspired format. All state lives in the task
file; there is no database, no daemon, no network. Written in Go, stdlib only,
hermetic deterministic tests.

## Revision history

- **v0.1 (2026-07-14): RELEASED as tag `v0.1`.** Everything in this document
  except the v0.2 items below is implemented, tested, and shipped on `main`.
- **v0.2 (2026-07-14): a DELTA on the shipped v0.1 codebase.** Work items
  should cover ONLY what is new; the existing code already satisfies the
  rest of this document. New in v0.2:
  1. The **due-removal empty-text rule** in `due` (owner ruling 2026-07-14).
     NOTE: this rule is ALREADY IMPLEMENTED and regression-tested on `main`
     (it was decided mid-v0.1 at the issue level); it is folded into this
     document so the requirements match shipped behavior. **No new work item
     should arise from it.**
  2. `list --archived` — read `done.txt` (new capability).
  3. `taskq edit` — replace a task's text (new command).
  4. `taskq projects` and `taskq contexts` — distinct-token listings (new
     commands).
  Everything else — grammar, atomicity, `--today`, the seven v0.1 commands,
  exit-code classes — is v0.1 text, unchanged and already shipped.

## The task file

- Default path `./todo.txt`; overridden by `--file <path>` (global flag,
  before the subcommand) or the `TASKQ_FILE` environment variable (flag wins).
  Completed tasks archive to `done.txt` **in the same directory as the task
  file** (whatever its name).
- **Line grammar** (one task per line, fields space-separated, order fixed):

  ```
  task      = [completion] [priority] [creation-date] text
  completion= "x " completion-date " "        (completed tasks only)
  priority  = "(" A-Z ")" " "                 (uppercase only)
  date      = YYYY-MM-DD                      (zero-padded, calendar-valid)
  text      = rest of line; may contain +project @context due:YYYY-MM-DD tokens
  ```

  Examples:
  - `(A) 2026-07-14 call the bank +errands @phone due:2026-07-20`
  - `x 2026-07-15 2026-07-14 call the bank +errands @phone due:2026-07-20`

  A completed task is `x `, then the completion date, then the original task
  line minus its priority (priority is dropped on completion — decided).
- **Task identity = 1-based line number** in the CURRENT task file, as shown
  by `list --all`. Numbers are not stable across mutations; every mutating
  command prints the affected task's line afterward, so the user re-lists to
  act again. (Decided: no persistent IDs in v0.1.)
- **Malformed lines** (violating the grammar above, or blank): preserved
  byte-for-byte in place through every rewrite, never counted as tasks,
  reported once per command on stderr as `taskq: skipped N malformed line(s)`.
  A mutation addressed AT a malformed line (by number) is a usage error.
  Line numbers count ALL lines (malformed included), so numbering matches a
  text editor.
- **Atomicity:** every mutation rewrites the whole file via temp file +
  `rename(2)` in the task file's directory (same filesystem). `archive` is the
  only two-file mutation; its order is decided: (1) append completed tasks to
  `done.txt` and flush, (2) rewrite the task file without them. A crash
  between the steps may duplicate tasks into both files but can never lose
  one; this is the accepted failure mode and must be documented in `--help`
  for `archive`.
- **Trailing newline:** the rewritten file always ends with exactly one `\n`
  (empty file = zero bytes).

## Today, injectable

Every date the tool WRITES or COMPARES AGAINST "today" (creation dates,
completion dates, `--overdue`) uses the value of the global flag
`--today YYYY-MM-DD` when given, else the system date in local time. Tests
always pass `--today`, so all behavior is hermetic. An invalid `--today` is a
usage error.

## Commands

Global flags (`--file`, `--today`) come before the subcommand (Go flag
convention, as in logq). Listings go to stdout; diagnostics to stderr.

- `taskq add <text...>` — append one task. Joins arguments with single
  spaces. Prepends today's date as creation date. If the text begins with
  `(A) `–`(Z) `, that is parsed as priority (kept ahead of the creation date
  per the grammar). Prints the added line prefixed by its line number.
  Adding an empty text is a usage error.
- `taskq list [--project P] [--context C] [--overdue] [--sort pri|due|created] [--all | --done | --archived]`
  — print tasks, one per line, prefixed `N ` (the 1-based line number, no
  padding — decided). Default scope: OPEN tasks from the task file only.
  `--all` = open + completed from the task file; `--done` = completed tasks
  from the task file only (`--all` and `--done` conflict: usage error;
  neither reads `done.txt` — decided).
  **`--archived` (NEW in v0.2)** = tasks from `done.txt` (same directory as
  the task file) instead of the task file. `--archived` conflicts with
  `--all` and with `--done` (usage error). N is the 1-based line number IN
  `done.txt`; these numbers are for reading only — mutating commands never
  address `done.txt` (decided). It lists every well-formed task line
  regardless of completion state (`archive` only ever writes completed
  tasks, but a hand-edited `done.txt` is shown as it is — decided);
  malformed lines are skipped and reported per the standard stderr note. A
  missing `done.txt` is an empty listing, exit 0 (read-command convention).
  Filters and sorts below apply to `--archived` listings identically.
  Filters AND together: `--project P` keeps tasks containing token `+P`,
  `--context C` keeps tasks containing token `@C` (exact token match, case
  sensitive). `--overdue` keeps tasks with `due:` strictly before today.
  Default order: file order. `--sort pri`: priority A→Z, tasks WITHOUT
  priority after all prioritized ones; `--sort due`: earliest due date first,
  tasks WITHOUT due: after all dated ones; `--sort created`: earliest creation
  date first, tasks WITHOUT a creation date last (a completed task's creation
  date is the date following its completion date, when present). ALL sorts
  are stable (ties and no-value groups keep file order — decided; the #25
  lesson).
- `taskq done <n>` — mark task n complete: prefix `x <today> `, drop its
  priority. On an ALREADY-completed task: no-op, print the line unchanged,
  exit 0 with a stderr note `taskq: task N already done` (decided:
  idempotent). Prints the resulting line prefixed by its number.
- `taskq rm <n>` — delete task n (open or completed). Prints the deleted line
  prefixed `removed: `.
- `taskq pri <n> <A-Z|none>` — set task n's priority (replacing any), or
  `none` removes it. Lowercase letter input is accepted and uppercased
  (decided). On a completed task: usage error (completed tasks carry no
  priority). Prints the resulting line prefixed by its number.
- `taskq due <n> <YYYY-MM-DD|none>` — set or remove the `due:` token. If the
  task has one, replace it in place; if not, append it at end of text; `none`
  removes it. Multiple pre-existing `due:` tokens: replace the FIRST, remove
  the rest (decided). Works on open tasks only (completed: usage error).
  Prints the resulting line prefixed by its number.
  **Due-removal empty-text rule (v0.2 doc change; ALREADY SHIPPED — owner
  ruling 2026-07-14):** a removal (via `none`, or the multiple-`due:`
  cleanup) that would leave the task's text EMPTY is a usage error — exit 1,
  message to stderr, nothing written. A documented command must never
  produce a malformed line; matches the empty-`add` invariant.
- `taskq edit <n> <text...>` **(NEW in v0.2)** — replace task n's text (the
  `text` field of the grammar) with the arguments joined by single spaces;
  the completion marker, priority, and creation date (or its absence) are
  preserved unchanged. Works on open tasks only (completed: usage error —
  consistent with `pri`/`due`). An empty replacement text is a usage error
  (the empty-text invariant). If the new text begins with `(A) `–`(Z) `
  (an uppercase priority prefix), that is a usage error with a message
  pointing at `pri` — `edit` never parses or sets priority (decided). The
  new text MAY contain `+project`/`@context`/`due:` tokens; they are stored
  as written, not validated (decided: tokens are free text per the grammar).
  Prints the resulting line prefixed by its number. Standard mutation rules
  apply (atomic rewrite, malformed lines preserved, addressed-at-malformed =
  usage error, missing file = I/O error `2`).
- `taskq projects` and `taskq contexts` **(NEW in v0.2)** — print every
  distinct `+project` (respectively `@context`) token found in OPEN tasks of
  the task file, one per line, WITH its sigil, byte-exact as written,
  deduplicated, sorted lexically (byte order). A token is a
  whitespace-delimited word beginning `+` (resp. `@`) with at least one
  following character (decided). Scope is fixed: open tasks of the task file
  only — no flags beyond the globals (decided). Empty result: no output,
  exit 0. Missing task file: empty, exit 0 (read-command convention).
- `taskq archive` — move ALL completed tasks to `done.txt` (append, file
  order, creating it if absent), rewrite the task file without them
  (malformed lines stay). Prints `archived N task(s)`. N=0 is fine (no file
  writes at all in that case — decided).

## Exit codes and errors

- `0` success (including empty results and idempotent no-ops).
- `1` usage error: unknown command/flag, bad task number (out of range,
  malformed line, wrong completion state), invalid date, empty add,
  empty or priority-prefixed `edit` text, due-removal that would empty a
  task's text, conflicting flags. Message to stderr, nothing written.
- `2` I/O or file-format failure: unreadable/unwritable file, rename failure.
  Message to stderr. A missing task file is NOT an error for read commands
  (empty listing) and is created by `add` (decided); it IS an error (`2`) for
  mutations that address a task by number (`done`/`rm`/`pri`/`due`/`edit` —
  there is nothing to address). A missing `done.txt` is NOT an error for
  `list --archived` (empty listing).
- Every error message starts `taskq: `.

## Out of scope (v0.2 — explicitly)

- Recurrence, reminders, notifications, time-of-day, timezones.
- Concurrent-writer safety beyond atomic rename (no locking).
- MUTATING `done.txt` (un-archive, `rm`/`edit` on archived tasks; `done.txt`
  remains append-only for `archive`, read-only for `list --archived`).
- Filters or flags on `projects`/`contexts` (fixed open-task scope in v0.2).
- Color/TTY detection, config files, shell completion, TUI.
- Undo, sync, import/export.
- Network, subprocesses, non-stdlib dependencies.

## Acceptance style (for the work-item graph)

Every work item must carry an acceptance checklist whose entries are
observable via `go test` (hermetic, table-driven, committed fixtures, always
`--today`-pinned) or via CLI invocations with byte-exact expected output.
The product-level acceptance matrix must cover: grammar round-trip
(parse→render byte-identity for well-formed lines), malformed-line
preservation through a mutation, atomic-rename behavior (temp file cleaned
up, content complete), every command's happy path + its decided edge cases
(idempotent `done`, priority drop on completion, stable sort ties,
archive-empty no-op, due-removal empty-text usage error, `--archived`
conflicts and missing-`done.txt` empty listing, `edit` field preservation,
`projects`/`contexts` dedup+order), `--today` injection, and both exit-code
classes. **The v0.2 matrix must cover the v0.1 capabilities as well as the
new ones** — the integration walk is the regression gate proving the
revision did not break shipped behavior.
