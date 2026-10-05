package scan

import (
	"fmt"
	"io"
	"strings"
)

const (
	// maxTableWidth caps the width of one report line, borders included.
	maxTableWidth = 80
	// minLastColumn keeps the last column readable when the other columns are
	// wide.
	minLastColumn = 24
)

// The box drawing parts. The rule uses the parts of one style, so the table
// looks the same in every terminal.
const (
	vertical   = "│"
	horizontal = "─"
)

var ruleStyles = map[string][3]string{
	"top":    {"┌", "┬", "┐"},
	"middle": {"├", "┼", "┤"},
	"bottom": {"└", "┴", "┘"},
}

// table prints text in a bordered grid. Every column but the last takes its
// natural width. The last column takes the rest of maxTableWidth, and its text
// wraps to that width.
type table struct {
	headers []string
	rows    [][]string
}

// newTable makes a table with one header per column name.
func newTable(headers ...string) *table {
	return &table{headers: headers}
}

// add appends one row. Extra cells are dropped, missing cells stay empty.
func (t *table) add(cells ...string) {
	row := make([]string, len(t.headers))
	copy(row, cells)
	t.rows = append(t.rows, row)
}

// widths returns the width of each column. Every column but the last takes
// its natural width. The last column takes its natural width, up to what is
// left of maxTableWidth.
func (t *table) widths() []int {
	last := len(t.headers) - 1
	widths := make([]int, len(t.headers))
	for i, h := range t.headers {
		widths[i] = displayWidth(h)
	}
	natural := widths[last]
	for _, row := range t.rows {
		for i, cell := range row {
			w := widestLine(cell)
			if i == last {
				natural = max(natural, w)
				continue
			}
			widths[i] = max(widths[i], w)
		}
	}
	room := maxTableWidth - (sum(widths[:last]) + borderSpace(len(t.headers)))
	widths[last] = min(natural, max(room, minLastColumn))
	return widths
}

// borderSpace returns the columns the borders and the padding take from the
// line width.
func borderSpace(columns int) int {
	return 3*columns + 1
}

func sum(values []int) int {
	total := 0
	for _, v := range values {
		total += v
	}
	return total
}

// widestLine returns the display width of the widest line of a cell.
func widestLine(cell string) int {
	width := 0
	for _, line := range strings.Split(cell, "\n") {
		if w := displayWidth(line); w > width {
			width = w
		}
	}
	return width
}

// print writes the bordered table.
func (t *table) print(w io.Writer) {
	widths := t.widths()
	t.printRule(w, widths, "top")
	t.printCells(w, widths, t.headerCells())
	t.printRule(w, widths, "middle")
	for i, row := range t.wrapRows(widths) {
		t.printCells(w, widths, row)
		if i < len(t.rows)-1 {
			t.printRule(w, widths, "middle")
		}
	}
	t.printRule(w, widths, "bottom")
}

// headerCells returns the styled header as one cell per column.
func (t *table) headerCells() [][]string {
	cells := make([][]string, len(t.headers))
	for i, h := range t.headers {
		cells[i] = []string{bold(h)}
	}
	return cells
}

// wrapRows wraps every cell to its column width, so each row becomes the list
// of its printed lines.
func (t *table) wrapRows(widths []int) [][][]string {
	rows := make([][][]string, len(t.rows))
	for r, row := range t.rows {
		lines := make([][]string, len(t.headers))
		for i, cell := range row {
			lines[i] = wrapCell(cell, widths[i])
		}
		rows[r] = lines
	}
	return rows
}

// printCells writes one row. The cells that have fewer lines than the row
// leave their column empty.
func (t *table) printCells(w io.Writer, widths []int, row [][]string) {
	height := 0
	for _, cell := range row {
		if len(cell) > height {
			height = len(cell)
		}
	}
	for line := 0; line < height; line++ {
		t.printLine(w, widths, row, line)
	}
}

// printLine writes one physical line between the borders. Every cell is padded
// to its column width, so the borders stay in one column.
func (t *table) printLine(w io.Writer, widths []int, row [][]string, line int) {
	var b strings.Builder
	b.WriteString(gray(vertical))
	for i, width := range widths {
		b.WriteString(" ")
		if line < len(row[i]) {
			b.WriteString(pad(row[i][line], width))
		} else {
			b.WriteString(strings.Repeat(" ", width))
		}
		b.WriteString(" ")
		b.WriteString(gray(vertical))
	}
	fmt.Fprintln(w, b.String())
}

// printRule writes one horizontal line. The style gives the corners and the
// joints: "top", "middle", or "bottom".
func (t *table) printRule(w io.Writer, widths []int, style string) {
	parts := ruleStyles[style]
	var b strings.Builder
	b.WriteString(gray(parts[0]))
	for i, width := range widths {
		if i > 0 {
			b.WriteString(gray(parts[1]))
		}
		b.WriteString(gray(strings.Repeat(horizontal, width+2)))
	}
	b.WriteString(gray(parts[2]))
	fmt.Fprintln(w, b.String())
}

// wrapCell splits a cell on its line breaks and wraps each line.
func wrapCell(cell string, width int) []string {
	var out []string
	for _, line := range strings.Split(cell, "\n") {
		out = append(out, wrapLine(line, width)...)
	}
	return out
}

// wrapLine breaks one line into lines of at most the width. It breaks at spaces
// and splits a word that is longer than the width.
//
// Every returned line repeats the style of the input. A cell that is one styled
// run cannot simply be cut in half, because the border printed between two
// wrapped lines ends with a full reset that clears the attributes the cell left
// open. The continuation would print in the default color.
func wrapLine(line string, width int) []string {
	open, text, close := splitStyle(line)
	pieces := wrapText(text, width)
	for i, piece := range pieces {
		suffix := ""
		if i == len(pieces)-1 {
			suffix = close
		}
		pieces[i] = open + piece + suffix
	}
	return pieces
}

// wrapText breaks plain text at spaces and splits an over-long word.
func wrapText(text string, width int) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{""}
	}
	var out []string
	current := ""
	for _, word := range words {
		for _, part := range breakWord(word, width) {
			switch {
			case current == "":
				current = part
			case displayWidth(current)+1+displayWidth(part) <= width:
				current += " " + part
			default:
				out = append(out, current)
				current = part
			}
		}
	}
	return append(out, current)
}

// splitStyle returns the escape sequences that open and close a line's style,
// and the plain text between them. A line with no styling is returned whole.
func splitStyle(line string) (open, text, close string) {
	spans := ansiCode.FindAllStringIndex(line, -1)
	if len(spans) == 0 {
		return "", line, ""
	}
	return line[:spans[0][1]], line[spans[0][1]:spans[len(spans)-1][0]], line[spans[len(spans)-1][0]:]
}

// breakWord splits a word that does not fit into pieces of the width.
func breakWord(word string, width int) []string {
	if displayWidth(word) <= width {
		return []string{word}
	}
	var parts []string
	current := ""
	for _, r := range word {
		if displayWidth(current)+runeWidth(r) > width {
			parts = append(parts, current)
			current = ""
		}
		current += string(r)
	}
	return append(parts, current)
}
