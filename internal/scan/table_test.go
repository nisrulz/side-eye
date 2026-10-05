package scan

import (
	"bytes"
	"strings"
	"testing"

	"github.com/fatih/color"
)

// noColor turns the color off for one test and restores the previous setting.
func noColor(t *testing.T) {
	t.Helper()
	previous := color.NoColor
	color.NoColor = true
	t.Cleanup(func() { color.NoColor = previous })
}

// lines splits a printed table into its lines, without the trailing empty one.
func lines(s string) []string {
	return strings.Split(strings.TrimRight(s, "\n"), "\n")
}

func TestTableBordersAndJoints(t *testing.T) {
	noColor(t)
	var out bytes.Buffer
	tbl := newTable("SEVERITY", "LOCATION")
	tbl.add("🔴 HIGH", "a.go:1")
	tbl.add("🟡 LOW", "b.go:2")
	tbl.print(&out)

	got := lines(out.String())
	want := []string{
		"┌──────────┬──────────┐",
		"│ SEVERITY │ LOCATION │",
		"├──────────┼──────────┤",
		"│ 🔴 HIGH  │ a.go:1   │",
		"├──────────┼──────────┤",
		"│ 🟡 LOW   │ b.go:2   │",
		"└──────────┴──────────┘",
	}
	if len(got) != len(want) {
		t.Fatalf("want %d lines, got %d: %q", len(want), len(got), out.String())
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d:\nwant %q\n got %q", i, want[i], got[i])
		}
	}
}

func TestTableLinesShareOneWidth(t *testing.T) {
	noColor(t)
	var out bytes.Buffer
	tbl := newTable("SEVERITY", "LOCATION", "FINDING")
	tbl.add("🚨 CRITICAL", "scripts/install.sh:10", "Downloads a binary and runs it.")
	tbl.add("🟠 MEDIUM", "a.go:1", "Short one.")
	tbl.print(&out)

	for _, line := range lines(out.String()) {
		if got := displayWidth(line); got > maxTableWidth {
			t.Errorf("line is %d columns, want at most %d: %q", got, maxTableWidth, line)
		}
		if got, want := displayWidth(line), displayWidth(lines(out.String())[0]); got != want {
			t.Errorf("line is %d columns, want %d: %q", got, want, line)
		}
	}
}

func TestTableWrapsLongText(t *testing.T) {
	noColor(t)
	var out bytes.Buffer
	tbl := newTable("SEVERITY", "LOCATION", "FINDING")
	tbl.add("🔴 HIGH", "Makefile:41", strings.Repeat("long detail text ", 40))
	tbl.print(&out)

	printed := lines(out.String())
	if len(printed) < 8 {
		t.Fatalf("long text must wrap over several lines, got %d", len(printed))
	}
	for _, line := range printed[3:] {
		if strings.Contains(line, strings.Repeat("long detail text", 2)) {
			t.Errorf("line holds more text than the column width: %q", line)
		}
	}
}

func TestTableSplitsAWordThatDoesNotFit(t *testing.T) {
	noColor(t)
	got := wrapLine(strings.Repeat("a", 25), 10)
	want := []string{"aaaaaaaaaa", "aaaaaaaaaa", "aaaaa"}
	if len(got) != len(want) {
		t.Fatalf("want %d pieces, got %d: %q", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("piece %d: want %q, got %q", i, want[i], got[i])
		}
	}
}

func TestTableKeepsLineBreakInACell(t *testing.T) {
	noColor(t)
	var out bytes.Buffer
	tbl := newTable("SEVERITY", "FINDING")
	tbl.add("🟡 LOW", "Title\ndetail")
	tbl.print(&out)

	printed := lines(out.String())
	if len(printed) != 6 {
		t.Fatalf("want header, rule, 2 cell lines and the bottom, got %d: %q", len(printed), out.String())
	}
	if !strings.Contains(printed[4], "detail") || strings.Contains(printed[4], "LOW") {
		t.Errorf("second cell line is wrong: %q", printed[4])
	}
}

func TestDisplayWidthIgnoresColorCodes(t *testing.T) {
	previous := color.NoColor
	color.NoColor = false
	t.Cleanup(func() { color.NoColor = previous })

	if got, want := displayWidth(red("HIGH")), 4; got != want {
		t.Errorf("displayWidth counted the escape codes: got %d, want %d", got, want)
	}
}

func TestEmojiTakesTwoColumns(t *testing.T) {
	if got := displayWidth("🔴"); got != 2 {
		t.Errorf("want 2 columns for an emoji, got %d", got)
	}
}

func TestPrintReportColorsSeverityAndTitle(t *testing.T) {
	previous := color.NoColor
	color.NoColor = false
	t.Cleanup(func() { color.NoColor = previous })

	findings := []Finding{
		{Severity: SeverityHigh, Path: "a.go", Line: 3, Title: "Runs on clone", Detail: "It downloads code."},
	}
	var out bytes.Buffer
	printReport(&out, "/root", findings)

	text := out.String()
	if !strings.Contains(text, "\x1b[") {
		t.Fatalf("color must be on, got plain output: %q", text)
	}
	for _, want := range []string{"HIGH", "a.go:3", "Runs on clone", "It downloads code.", severityAction(SeverityHigh)} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %q", want, text)
		}
	}
}

// TestWrappedCellKeepsItsStyle checks every line of a wrapped cell carries the
// style of the cell. The border between two lines ends with a full reset, so a
// cell that opened its style once and closed it on the last line loses the color
// on the lines in between.
func TestWrappedCellKeepsItsStyle(t *testing.T) {
	previous := color.NoColor
	color.NoColor = false
	t.Cleanup(func() { color.NoColor = previous })

	detail := "It holds the local SDK path and should stay out of the project"
	findings := []Finding{
		{Severity: SeverityLow, Path: "local.properties", Title: "local.properties present", Detail: detail},
	}
	var out bytes.Buffer
	printReport(&out, "/root", findings)

	var styled int
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.Contains(line, "local SDK path") || strings.Contains(line, "out of the project") {
			styled++
			if !strings.Contains(line, "\x1b[2m") {
				t.Errorf("wrapped line lost the dim style: %q", line)
			}
		}
	}
	if styled != 2 {
		t.Errorf("want the detail on two lines, found %d in %q", styled, out.String())
	}
}

// TestWrapLineRepeatsStyle checks the wrapping itself, without the table around
// it. The opening code goes on every piece, so each line restyles itself after
// the border reset, and the closing code goes only on the last.
func TestWrapLineRepeatsStyle(t *testing.T) {
	previous := color.NoColor
	color.NoColor = false
	t.Cleanup(func() { color.NoColor = previous })

	lines := wrapLine(dim("one two three four five six"), 12)
	want := []string{
		"\x1b[2mone two",
		"\x1b[2mthree four",
		"\x1b[2mfive six\x1b[22m",
	}
	if len(lines) != len(want) {
		t.Fatalf("wrapLine = %q, want %d lines", lines, len(want))
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
}

func TestSplitStyle(t *testing.T) {
	open, text, close := splitStyle("\x1b[2mplain text\x1b[22m")
	if open != "\x1b[2m" || text != "plain text" || close != "\x1b[22m" {
		t.Errorf("splitStyle = %q, %q, %q", open, text, close)
	}
	open, text, close = splitStyle("no styling here")
	if open != "" || text != "no styling here" || close != "" {
		t.Errorf("splitStyle on plain text = %q, %q, %q", open, text, close)
	}
}

func TestPrintReportPlainWhenColorIsOff(t *testing.T) {
	noColor(t)
	var out bytes.Buffer
	printReport(&out, "/root", []Finding{{Severity: SeverityLow, Path: "b.sh", Title: "Runs on commit"}})

	if strings.Contains(out.String(), "\x1b[") {
		t.Errorf("color must be off: %q", out.String())
	}
	if !strings.Contains(out.String(), "🟡 LOW") {
		t.Errorf("severity marker missing: %q", out.String())
	}
}

func TestPrintReportCleanScan(t *testing.T) {
	noColor(t)
	var out bytes.Buffer
	printReport(&out, "/root", nil)

	want := "✅ No code that runs on clone, open, or commit in /root\n"
	if out.String() != want {
		t.Errorf("want %q, got %q", want, out.String())
	}
}
