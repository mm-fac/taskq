package main

import "testing"

// TestRoundTripWellFormed proves parse→render byte-identity for well-formed
// lines across every field combination named in the acceptance checklist.
func TestRoundTripWellFormed(t *testing.T) {
	cases := []struct {
		name string
		line string
	}{
		{"text only", "call the bank +errands @phone due:2026-07-20"},
		{"priority+text", "(A) call the bank"},
		{"creation-date+text", "2026-07-14 call the bank"},
		{"priority+creation-date+text", "(A) 2026-07-14 call the bank +errands @phone due:2026-07-20"},
		{"completed with creation-date", "x 2026-07-15 2026-07-14 call the bank +errands @phone due:2026-07-20"},
		{"completed without creation-date", "x 2026-07-15 call the bank"},
		{"lowest priority", "(Z) do the thing"},
		// Odd but grammar-legal spacing survives the round-trip because the
		// surplus space becomes part of the text field.
		{"extra space after priority", "(A)  2026-07-14 spaced"},
		// A leading "x " that is not a valid completion marker is plain text.
		{"x-prefixed text", "x not a completion"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ParseLine(c.line)
			if got.Malformed {
				t.Fatalf("ParseLine(%q) classified malformed, want well-formed", c.line)
			}
			if r := got.Render(); r != c.line {
				t.Errorf("round-trip mismatch:\n input  %q\n render %q", c.line, r)
			}
		})
	}
}

// TestParseFields checks that a well-formed line exposes exactly the parts later
// items rely on, with text tokens left intact.
func TestParseFields(t *testing.T) {
	cases := []struct {
		name string
		line string
		want Task
	}{
		{
			name: "priority + creation date",
			line: "(A) 2026-07-14 call the bank +errands @phone due:2026-07-20",
			want: Task{
				Priority:     'A',
				CreationDate: "2026-07-14",
				Text:         "call the bank +errands @phone due:2026-07-20",
			},
		},
		{
			name: "completed with completion + creation date",
			line: "x 2026-07-15 2026-07-14 call the bank +errands @phone due:2026-07-20",
			want: Task{
				Completed:      true,
				CompletionDate: "2026-07-15",
				CreationDate:   "2026-07-14",
				Text:           "call the bank +errands @phone due:2026-07-20",
			},
		},
		{
			name: "completed without creation date",
			line: "x 2026-07-15 call the bank",
			want: Task{
				Completed:      true,
				CompletionDate: "2026-07-15",
				Text:           "call the bank",
			},
		},
		{
			name: "text only leaves tokens untouched",
			line: "buy milk +home @store due:2026-07-20",
			want: Task{Text: "buy milk +home @store due:2026-07-20"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ParseLine(c.line)
			if got.Malformed {
				t.Fatalf("ParseLine(%q) malformed, want well-formed", c.line)
			}
			if got.Completed != c.want.Completed ||
				got.CompletionDate != c.want.CompletionDate ||
				got.Priority != c.want.Priority ||
				got.CreationDate != c.want.CreationDate ||
				got.Text != c.want.Text {
				t.Errorf("field mismatch for %q\n got  %+v\n want %+v", c.line, got, c.want)
			}
		})
	}
}

// TestPriorityParsing covers the priority rules: only a single uppercase letter
// in parentheses followed by a space is a priority; everything else stays text.
func TestPriorityParsing(t *testing.T) {
	cases := []struct {
		name         string
		line         string
		wantPriority byte // 0 == none
		wantText     string
	}{
		{"valid A", "(A) buy milk", 'A', "buy milk"},
		{"valid Z", "(Z) buy milk", 'Z', "buy milk"},
		{"lowercase not a priority", "(a) buy milk", 0, "(a) buy milk"},
		{"double letter not a priority", "(AA) buy milk", 0, "(AA) buy milk"},
		{"digit not a priority", "(1) buy milk", 0, "(1) buy milk"},
		{"missing trailing space", "(A)buy milk", 0, "(A)buy milk"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ParseLine(c.line)
			if got.Malformed {
				t.Fatalf("ParseLine(%q) malformed, want well-formed", c.line)
			}
			if got.Priority != c.wantPriority {
				t.Errorf("ParseLine(%q).Priority = %q, want %q", c.line, rune0(got.Priority), rune0(c.wantPriority))
			}
			if got.Text != c.wantText {
				t.Errorf("ParseLine(%q).Text = %q, want %q", c.line, got.Text, c.wantText)
			}
		})
	}
}

// rune0 renders a priority byte for error messages (0 shows as "none").
func rune0(b byte) string {
	if b == 0 {
		return "none"
	}
	return string(rune(b))
}

// TestValidDate is a table of valid and invalid date samples, per acceptance.
func TestValidDate(t *testing.T) {
	cases := []struct {
		date string
		want bool
	}{
		{"2026-07-14", true},
		{"2026-01-01", true},
		{"2026-12-31", true},
		{"2026-02-28", true},
		{"2024-02-29", true}, // leap year
		{"2000-02-29", true}, // divisible by 400
		{"2026-02-30", false},
		{"2026-13-01", false},
		{"2026-00-01", false},
		{"2026-01-00", false},
		{"2023-02-29", false}, // not a leap year
		{"2100-02-29", false}, // century, not divisible by 400
		{"2026-7-14", false},  // not zero-padded month
		{"2026-07-4", false},  // not zero-padded day
		{"26-07-14", false},   // short year
		{"2026/07/14", false}, // wrong separator
		{"20260714", false},   // no separators
		{"2026-07-14 ", false},
		{"", false},
	}

	for _, c := range cases {
		if got := validDate(c.date); got != c.want {
			t.Errorf("validDate(%q) = %v, want %v", c.date, got, c.want)
		}
	}
}

// TestDatesInLines confirms a valid creation date is recognized while an
// invalid one falls through to become part of the text.
func TestDatesInLines(t *testing.T) {
	if got := ParseLine("2026-07-14 buy milk"); got.CreationDate != "2026-07-14" {
		t.Errorf("valid creation date not recognized: %+v", got)
	}
	// 2026-02-30 is not calendar-valid, so it cannot be a creation date; the
	// whole line becomes text.
	got := ParseLine("2026-02-30 buy milk")
	if got.CreationDate != "" || got.Text != "2026-02-30 buy milk" {
		t.Errorf("invalid date should fall through to text, got %+v", got)
	}
}

// TestMalformed asserts malformed classification and byte-for-byte preservation
// of the original input.
func TestMalformed(t *testing.T) {
	cases := []struct {
		name string
		line string
	}{
		{"empty", ""},
		{"single space", " "},
		{"spaces only", "     "},
		{"tab only", "\t"},
		{"priority with no text", "(A) "},
		{"date with no text", "2026-07-14 "},
		{"completed with no text", "x 2026-07-15 "},
		{"completed date with no text", "x 2026-07-15 2026-07-14 "},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ParseLine(c.line)
			if !got.Malformed {
				t.Fatalf("ParseLine(%q) classified well-formed, want malformed (%+v)", c.line, got)
			}
			if got.Raw != c.line {
				t.Errorf("ParseLine(%q).Raw = %q, want verbatim input", c.line, got.Raw)
			}
			if r := got.Render(); r != c.line {
				t.Errorf("Render of malformed changed bytes: got %q, want %q", r, c.line)
			}
		})
	}
}

// TestWellFormedNotMalformed guards against over-eager malformed classification
// for lines that are legal text.
func TestWellFormedNotMalformed(t *testing.T) {
	for _, line := range []string{
		"hello",
		"(a) lowercase stays text",
		"x still text",
		"2026-13-01 bad date is just text",
		"   leading spaces then text",
	} {
		if got := ParseLine(line); got.Malformed {
			t.Errorf("ParseLine(%q) unexpectedly malformed", line)
		}
	}
}
