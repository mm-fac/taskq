package main

import (
	"strings"
	"time"
)

// Task is the parsed form of a single line of the task file.
//
// The line grammar (requirements.md, "The task file") is:
//
//	task       = [completion] [priority] [creation-date] text
//	completion = "x " completion-date " "   (completed tasks only)
//	priority   = "(" A-Z ")" " "            (uppercase only)
//	date       = YYYY-MM-DD                 (zero-padded, calendar-valid)
//	text       = rest of line; may contain +project @context due:YYYY-MM-DD tokens
//
// A line that does not satisfy the grammar (including a blank/empty line, or a
// line consisting solely of the structured prefixes with no text) is
// classified Malformed; its original bytes are preserved verbatim in Raw and no
// other field is meaningful. Downstream code must keep malformed lines
// byte-for-byte.
//
// For a well-formed Task, Render reproduces the original line byte-for-byte.
type Task struct {
	// Raw is the original line exactly as parsed, always populated.
	Raw string
	// Malformed reports that the line violated the grammar; only Raw is valid.
	Malformed bool

	// Completed reports the leading "x <completion-date> " marker.
	Completed bool
	// CompletionDate is the "YYYY-MM-DD" completion date, set iff Completed.
	CompletionDate string
	// Priority is the priority letter 'A'-'Z', or 0 when there is none.
	Priority byte
	// CreationDate is the "YYYY-MM-DD" creation date, or "" when absent.
	CreationDate string
	// Text is the trailing free text, with any +project/@context/due: tokens
	// left inside it unmodified. Never empty for a well-formed Task.
	Text string
}

// ParseLine parses a single raw line (without its trailing newline) into a
// Task. It never returns an error: a line that does not match the grammar is
// returned with Malformed set and Raw holding the input verbatim.
func ParseLine(raw string) Task {
	t := Task{Raw: raw}

	// Blank or empty lines are malformed.
	if strings.TrimSpace(raw) == "" {
		t.Malformed = true
		return t
	}

	rest := raw

	// [completion] = "x " completion-date " "
	if strings.HasPrefix(rest, "x ") {
		after := rest[2:]
		if len(after) >= 11 && after[10] == ' ' && validDate(after[:10]) {
			t.Completed = true
			t.CompletionDate = after[:10]
			rest = after[11:]
		}
	}

	// [priority] = "(" A-Z ")" " "
	if len(rest) >= 4 && rest[0] == '(' && rest[2] == ')' && rest[3] == ' ' &&
		rest[1] >= 'A' && rest[1] <= 'Z' {
		t.Priority = rest[1]
		rest = rest[4:]
	}

	// [creation-date] = date " "
	if len(rest) >= 11 && rest[10] == ' ' && validDate(rest[:10]) {
		t.CreationDate = rest[:10]
		rest = rest[11:]
	}

	// text = rest of line; a well-formed task must carry non-empty text.
	if rest == "" {
		return Task{Raw: raw, Malformed: true}
	}
	t.Text = rest
	return t
}

// Render reproduces the exact line text for a well-formed Task, byte-for-byte
// with the line it was parsed from. For a malformed Task it returns the
// preserved raw bytes.
func (t Task) Render() string {
	if t.Malformed {
		return t.Raw
	}

	var b strings.Builder
	if t.Completed {
		b.WriteString("x ")
		b.WriteString(t.CompletionDate)
		b.WriteByte(' ')
	}
	if t.Priority != 0 {
		b.WriteByte('(')
		b.WriteByte(t.Priority)
		b.WriteString(") ")
	}
	if t.CreationDate != "" {
		b.WriteString(t.CreationDate)
		b.WriteByte(' ')
	}
	b.WriteString(t.Text)
	return b.String()
}

// validDate reports whether s is a zero-padded, calendar-valid YYYY-MM-DD date.
// The explicit shape check enforces the zero-padding and layout (so "2026-7-14"
// is rejected), and time.Parse enforces calendar validity (so "2026-02-30" and
// "2026-13-01" are rejected).
func validDate(s string) bool {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return false
	}
	for i := 0; i < len(s); i++ {
		if i == 4 || i == 7 {
			continue
		}
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}
