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

// sanitizeText removes the escape sequences and control characters a terminal
// acts on. A finding carries text out of the scanned repository: a file name, a
// config value, a symlink target, an LLM reply. A terminal executes what it is
// given, so one of those values carrying ESC[2J ESC[H can clear the report and
// replace it with a clean-looking one. The tool exists to decide whether a
// repository is safe to open, so the reader must see the findings verbatim.
//
// It walks the string instead of matching a pattern, because a regex cannot
// describe every sequence a terminal accepts and a near miss passes the payload
// straight through. Newlines stay: the table already treats them as the line
// breaks inside a cell, and it redraws the borders around each one, so they
// cannot forge a row.
func sanitizeText(s string) string {
	if !needsSanitize(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		switch c := s[i]; {
		case c == 0x1b:
			i = skipEscape(s, i)
		case c == '\n':
			b.WriteByte(c)
			i++
		case c < 0x20 || c == 0x7f:
			i++
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

// needsSanitize reports if the string holds anything a terminal would act on.
// Most findings are plain text, so the scan stays a single pass over the bytes.
func needsSanitize(s string) bool {
	for i := 0; i < len(s); i++ {
		if c := s[i]; c == 0x1b || (c < 0x20 && c != '\n') || c == 0x7f {
			return true
		}
	}
	return false
}

// skipEscape returns the index just past the escape sequence at s[i], which
// always starts with ESC. An unterminated sequence runs to the end of the
// string, because a truncated sequence still moves a real terminal.
func skipEscape(s string, i int) int {
	j := i + 1
	if j >= len(s) {
		return j
	}
	switch s[j] {
	case '[': // CSI: parameters, then one final byte.
		j++
		for j < len(s) && s[j] >= 0x20 && s[j] <= 0x3f {
			j++
		}
		if j < len(s) {
			j++
		}
		return j
	case ']', 'P', 'X', '^', '_': // OSC, DCS, SOS, PM, APC: run to BEL or ST.
		j++
		for j < len(s) {
			switch {
			case s[j] == 0x07:
				return j + 1
			case s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\':
				return j + 2
			}
			j++
		}
		return j
	default: // Fe and Fp escapes: any intermediates, then one final byte.
		for j < len(s) && s[j] >= 0x20 && s[j] <= 0x2f {
			j++
		}
		if j < len(s) {
			j++
		}
		return j
	}
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
