package scan

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/fatih/color"
)

// setColor turns ANSI color on or off for the whole run. Color stays off for
// JSON output, for a writer that is not a terminal, and when NO_COLOR is set.
func setColor(w io.Writer, jsonOut bool) {
	color.NoColor = jsonOut || os.Getenv("NO_COLOR") != "" || !isTerminal(w)
}

func styled(attrs ...color.Attribute) func(a ...any) string {
	return color.New(attrs...).SprintFunc()
}

var (
	bold   = styled(color.Bold)
	dim    = styled(color.Faint)
	red    = styled(color.FgRed)
	green  = styled(color.FgGreen)
	yellow = styled(color.FgYellow)
	cyan   = styled(color.FgCyan)
	gray   = styled(color.FgHiBlack)
)

// severityStyles give each severity its own color. The map is read only.
var severityStyles = map[Severity]func(a ...any) string{
	SeverityCritical: styled(color.Bold, color.FgRed),
	SeverityHigh:     styled(color.Bold, color.FgRed),
	SeverityMedium:   styled(color.FgYellow),
	SeverityLow:      styled(color.FgCyan),
	SeverityInfo:     gray,
}

// severityEmoji is the marker that lets a reader scan the severity column
// without reading the words.
func severityEmoji(s Severity) string {
	switch s {
	case SeverityCritical:
		return "🚨"
	case SeverityHigh:
		return "🔴"
	case SeverityMedium:
		return "🟠"
	case SeverityLow:
		return "🟡"
	default:
		return "⚪"
	}
}

// severityStyle returns the color of one severity.
func severityStyle(s Severity) func(a ...any) string {
	style, ok := severityStyles[s]
	if !ok {
		return gray
	}
	return style
}

// severityText renders the emoji and the severity name in the severity color.
func severityText(s Severity) string {
	return severityStyle(s)(severityEmoji(s) + " " + s.String())
}

// printCommand writes one indented command line, so every command in the advice
// looks the same.
func printCommand(w io.Writer, format string, a ...any) {
	fmt.Fprintf(w, "    %s\n", cyan(fmt.Sprintf(format, a...)))
}

// failf writes one error line with a red marker.
func failf(w io.Writer, format string, a ...any) {
	fmt.Fprintf(w, "%s %s\n", red("✗"), fmt.Sprintf(format, a...))
}

// ansiCode matches the color escape sequences that padding must ignore.
var ansiCode = regexp.MustCompile("\x1b\\[[0-9;]*m")

// stripANSI removes the color escape sequences from one string.
func stripANSI(s string) string {
	return ansiCode.ReplaceAllString(s, "")
}

// runeWidth returns the terminal width of one rune. Emoji and East Asian
// runes take two columns, so the table lines up in every terminal.
func runeWidth(r rune) int {
	switch {
	case r >= 0x1F300 && r <= 0x1FAFF,
		r >= 0x2600 && r <= 0x27BF,
		r >= 0x1100 && r <= 0x115F,
		r >= 0x2E80 && r <= 0xA4CF,
		r >= 0xAC00 && r <= 0xD7A3,
		r >= 0xF900 && r <= 0xFAFF,
		r >= 0xFE30 && r <= 0xFE6F,
		r >= 0xFF00 && r <= 0xFF60,
		r >= 0xFFE0 && r <= 0xFFE6:
		return 2
	}
	return 1
}

// displayWidth returns the number of terminal columns one string takes, with
// the color escape sequences excluded.
func displayWidth(s string) int {
	width := 0
	for _, r := range stripANSI(s) {
		width += runeWidth(r)
	}
	return width
}

// pad right pads one cell to the column width. Escape sequences do not count,
// so a colored cell stays aligned.
func pad(s string, width int) string {
	if gap := width - displayWidth(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}
