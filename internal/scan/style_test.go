package scan

import (
	"strings"
	"testing"

	"github.com/fatih/color"
)

// TestSanitizeTextDropsTerminalSequences covers the sequences a terminal acts
// on. A file name, a config value, or an LLM reply carries them, and the report
// is the only thing standing between that text and the reader's screen.
func TestSanitizeTextDropsTerminalSequences(t *testing.T) {
	for name, payload := range map[string]string{
		"clear screen and home": "\x1b[2J\x1b[H",
		"erase line":            "\x1b[2K",
		"erase display":         "\x1b[J",
		"move cursor":           "\x1b[1;1H",
		"hide cursor":           "\x1b[?25l",
		"private mode":          "\x1b[?1049h",
		"osc title":             "\x1b]0;pwned\x07",
		"osc st terminated":     "\x1b]2;pwned\x1b\\",
		"osc clipboard":         "\x1b]52;c;cGF5ZWQ=\x07",
		"osc shell integration": "\x1b]133;A\x07",
		"dcs":                   "\x1bPq#0;2;0;0;0\x1b\\",
		"apc":                   "\x1b_Gf=100\x1b\\",
		"two character":         "\x1bc",
		"charset select":        "\x1b(0",
	} {
		if got := sanitizeText(payload); got != "" {
			t.Errorf("%s: sanitizeText(%q) = %q, want it empty", name, payload, got)
		}
	}
}

// TestSanitizeTextDropsControlCharacters checks the bytes that move the cursor
// or overwrite what is already on the line. A file name carrying a carriage
// return redraws the rest of the row over the finding.
func TestSanitizeTextDropsControlCharacters(t *testing.T) {
	for name, payload := range map[string]string{
		"carriage return": "safe.sh\r",
		"backspace":       "safe.sh\b\b\b",
		"bell":            "safe.sh\a",
		"vertical tab":    "safe.sh\v",
		"form feed":       "safe.sh\f",
		"null byte":       "safe.sh\x00",
		"delete":          "safe.sh\x7f",
	} {
		if got := sanitizeText(payload); got != "safe.sh" {
			t.Errorf("%s: sanitizeText(%q) = %q, want %q", name, payload, got, "safe.sh")
		}
	}
}

// TestSanitizeTextKeepsTextAroundThePayload checks the payload goes and the
// surrounding name stays. Stripping the whole cell would hide the finding, which
// is the same failure as letting the payload through.
func TestSanitizeTextKeepsTextAroundThePayload(t *testing.T) {
	for name, payload := range map[string]string{
		"escape in name": "setup.sh\x1b[2J\x1b[H\x1b[2KSAFE.sh",
		"return in name": "setup.sh\rSAFE.sh",
	} {
		if got := sanitizeText(payload); got != "setup.shSAFE.sh" {
			t.Errorf("%s: sanitizeText(%q) = %q, want %q", name, payload, got, "setup.shSAFE.sh")
		}
	}
}

// TestSanitizeTextKeepsReadableText checks the sanitizer only removes what a
// terminal would act on. A finding a reader has to act on stays intact.
func TestSanitizeTextKeepsReadableText(t *testing.T) {
	for _, want := range []string{
		"",
		"setup.sh",
		"Setup script pipes into an interpreter: | sh",
		"Attribute binds filter=lfs",
		"gradle/wrapper/gradle-wrapper.properties",
		"café/naïve/日本語.sh",
		"a > b && c < d",
		"emoji 🚨 and → arrows",
	} {
		if got := sanitizeText(want); got != want {
			t.Errorf("sanitizeText(%q) = %q, want it unchanged", want, got)
		}
	}
}

// TestSanitizeTextTruncatedSequence checks a sequence that never ends still goes.
// A half-written sequence moves a real terminal, so it cannot be passed on.
func TestSanitizeTextTruncatedSequence(t *testing.T) {
	for name, payload := range map[string]string{
		"bare escape":      "safe.sh\x1b",
		"open csi":         "safe.sh\x1b[",
		"open csi params":  "safe.sh\x1b[2",
		"open osc":         "safe.sh\x1b]0;",
		"open dcs":         "safe.sh\x1bP",
		"escape at start":  "\x1b[2J",
		"escape in middle": "a\x1b[2Jb",
	} {
		got := sanitizeText(payload)
		if strings.ContainsRune(got, 0x1b) {
			t.Errorf("%s: escape survived in %q", name, got)
		}
		if strings.Contains(got, "safe.sh") && got != "safe.sh" {
			t.Errorf("%s: sanitizeText(%q) = %q, want %q", name, payload, got, "safe.sh")
		}
	}
}

// TestSanitizeTextKeepsLineBreaks checks the table's own line breaks survive. A
// finding cell is built from several lines, and the borders are redrawn around
// each one, so a newline cannot forge a row.
func TestSanitizeTextKeepsLineBreaks(t *testing.T) {
	got := sanitizeText("title\n↳ detail\x1b[2J\nmore")
	want := "title\n↳ detail\nmore"
	if got != want {
		t.Errorf("sanitizeText = %q, want %q", got, want)
	}
}

// TestPrintReportSanitizesFindings is the end of the chain: a finding built from
// repository text reaches the terminal through printReport, so the report is
// checked for the payload rather than the sanitizer in isolation.
func TestPrintReportSanitizesFindings(t *testing.T) {
	noColor(t)
	findings := []Finding{{
		Severity: SeverityCritical,
		Path:     "setup.sh\x1b[2J\x1b[H\x1b[2KSAFE.sh",
		Line:     1,
		Title:    "Setup script pipes into | sh",
		Detail:   "Git runs the filter.\x1b[2J\x1b[H>>> AUDITED <<<",
	}}

	var out strings.Builder
	printReport(&out, "/root", findings)
	text := out.String()

	if strings.ContainsRune(text, 0x1b) {
		t.Errorf("color must be off and the payload stripped, got %q", text)
	}
	for _, want := range []string{
		"setup.shSAFE.sh:1",
		"Setup script pipes into | sh",
		"Git runs the filter.",
		">>> AUDITED <<<",
		"1 critical",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %q", want, text)
		}
	}
}

// TestPrintReportKeepsOurOwnStyling checks sanitizing the finding text did not
// take the report's own colors with it. The escapes the tool writes must reach
// the terminal, or the report loses its severity colors.
func TestPrintReportKeepsOurOwnStyling(t *testing.T) {
	previous := color.NoColor
	color.NoColor = false
	t.Cleanup(func() { color.NoColor = previous })

	var out strings.Builder
	printReport(&out, "/root", []Finding{{
		Severity: SeverityCritical,
		Path:     "setup.sh",
		Title:    "Setup script pipes into an interpreter: | sh",
	}})

	text := out.String()
	if !strings.Contains(text, "\x1b[") {
		t.Fatalf("the report's own color codes must survive: %q", text)
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "setup.sh") && !strings.Contains(line, "\x1b[") {
			t.Errorf("the location cell lost its styling: %q", line)
		}
	}
}
