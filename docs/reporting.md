# Reporting

## Finding

`findings.go` defines `Finding`:

| Field | JSON | Notes |
| --- | --- | --- |
| `Severity` | `severity` | Integer. Marshals through `String`. |
| `Path` | `path` | Absolute for local scans, repo-relative for remote scans |
| `Line` | `line` | Omitted when 0 |
| `Title` | `title` | Short label |
| `Detail` | `detail` | Omitted when empty |

`highestSeverity` returns the worst finding in a set.

The optional LLM pass adds findings of the same type, with the title prefixed `LLM: `. See [llm-scan.md](llm-scan.md).

## Human output

`printReport` in `report.go` does this:

1. `sortFindings` sorts by severity descending, then path, then line.
2. A header line states the count and the scan root.
3. A table prints one row per finding: severity, location, then the title, the detail, and the action as separate lines under the title column.
4. A summary line shows the count and the severity counts.

`severityEmoji` in `style.go` maps severity to a marker: `🚨` critical, `🔴` high, `🟠` medium, `🟡` low, `⚪` info.

A clean scan prints `✅ No code that runs on clone, open, or commit in <root>`.

`location` prints the path relative to the scan root when possible.

### Table

`table.go` has the bordered table renderer. Each column has its own natural width, except the last one. The last column takes what is left of `maxTableWidth`, so a table with long text is at most 80 columns wide. `wrapLine` breaks the text of a cell at spaces, and `breakWord` splits a word that does not fit.

`wrapLine` also keeps the color. A cell is one styled run, so cutting it in half would leave the escape sequence open across a line break. The border printed between two lines ends with a full reset, which clears the attributes the cell had set, so the continuation printed in the default color. `splitStyle` takes the opening and closing sequences off the line, `wrapText` breaks the plain text between them, and every piece is written back with the opening sequence. Only the last piece gets the closing one.

A cell holds several lines: the title, then the detail, then the action. They print under the finding title.

`displayWidth` ignores the ANSI escape sequences, so a colored cell stays aligned. `runeWidth` gives an emoji two columns.

### Color

`style.go` holds the color styles and uses `github.com/fatih/color`. `setColor` turns the color off for JSON output, for a writer that is not a terminal, and when `NO_COLOR` is set.

The banner is yellow. Each severity has its own color: red for critical and high, yellow for medium, cyan for low, gray for info.

## JSON output

`printJSON` writes one object:

```json
{
  "root": "owner/repo",
  "remote": true,
  "url": "owner/repo",
  "ref": "main",
  "findings": []
}
```

The `findings` array is always present, never `null`.

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | No finding at or above the threshold |
| 1 | A finding at or above `-fail-on` |
| 2 | Usage error or scan failure |

`parseSeverity` accepts `low`, `medium`, `high`, `critical`, and `none`.
`none` sets the threshold above `critical`, so the scan never returns exit code 1.
