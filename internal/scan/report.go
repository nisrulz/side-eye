package scan

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
)

// scanResult is one finished scan: the output form, the text report heading,
// the path the scan ran on, the advice for that target type, the findings, and
// the notes about the steps that did not add findings.
type scanResult struct {
	jsonOut  bool
	heading  string
	root     string
	advice   func(io.Writer)
	findings []Finding
	notes    []string
	remote   *remoteTarget
}

// printResult prints the findings as JSON, or as a text report under the
// heading and the advice. It returns false when the JSON output fails.
func printResult(stdout, stderr io.Writer, res scanResult) bool {
	if res.jsonOut {
		if err := printJSON(stdout, res.root, res.findings, res.remote); err != nil {
			failf(stderr, "%v", err)
			return false
		}
		printNotes(stderr, res.notes)
		return true
	}
	if res.heading != "" {
		fmt.Fprintf(stdout, "%s %s\n\n", dim("→"), bold(res.heading))
	}
	printReport(stdout, res.root, res.findings)
	if res.advice != nil {
		res.advice(stdout)
	}
	printNotes(stderr, res.notes)
	return true
}

// printNotes writes the notes after the report, so the reader sees the findings
// first and a skipped step never reads as a failed scan.
func printNotes(w io.Writer, notes []string) {
	for _, note := range notes {
		if note == "" {
			continue
		}
		for _, line := range strings.Split(note, "\n") {
			fmt.Fprintln(w, line)
		}
	}
}

func sortFindings(findings []Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Severity != findings[j].Severity {
			return findings[i].Severity > findings[j].Severity
		}
		if findings[i].Path != findings[j].Path {
			return findings[i].Path < findings[j].Path
		}
		return findings[i].Line < findings[j].Line
	})
}

func printReport(w io.Writer, root string, findings []Finding) {
	sortFindings(findings)
	if len(findings) == 0 {
		fmt.Fprintf(w, "%s No code that runs on clone, open, or commit in %s\n", green("✅"), cyan(root))
		return
	}

	fmt.Fprintf(w, "😒 side-eye found %s in %s\n\n", bold(plural(len(findings), "risky item")), cyan(root))
	t := newTable("SEVERITY", "LOCATION", "FINDING")
	for _, f := range findings {
		t.add(severityText(f.Severity), location(root, f), findingCell(f))
	}
	t.print(w)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "→ %s: %s\n", bold(plural(len(findings), "risky item")), severityCounts(findings))
}

// findingCell builds the text of one finding: the title, then the detail, then
// the action. Each part is its own line so it prints under the title column.
func findingCell(f Finding) string {
	parts := []string{bold(f.Title)}
	if f.Detail != "" {
		parts = append(parts, dim("↳ "+f.Detail))
	}
	return strings.Join(append(parts, yellow("👉 "+severityAction(f.Severity))), "\n")
}

func printJSON(w io.Writer, root string, findings []Finding, remote *remoteTarget) error {
	sortFindings(findings)
	if findings == nil {
		findings = []Finding{}
	}
	out := struct {
		Root     string    `json:"root"`
		Remote   bool      `json:"remote"`
		URL      string    `json:"url,omitempty"`
		Ref      string    `json:"ref,omitempty"`
		Findings []Finding `json:"findings"`
	}{Root: root, Findings: findings}
	if remote != nil {
		out.Remote = true
		out.URL = remote.raw
		out.Ref = remote.ref
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// printZipAdvice warns that a ZIP archive without a .git directory hides the
// config and hooks that exist only after a clone.
func printZipAdvice(w io.Writer, hasGit bool) {
	if hasGit {
		return
	}
	fmt.Fprintln(w, "\n"+dim("→ The archive has no .git directory, so .git/config and .git/hooks are not scanned."))
	fmt.Fprintln(w, dim("→ For a full local scan, clone without a working tree:"))
	printCommand(w, "git clone --no-checkout <url> <dir>")
	printCommand(w, "side-eye <dir>")
}

// printPlainAdvice names the checks a directory without a .git cannot run, so
// a clean result is not read as a clean repository.
func printPlainAdvice(w io.Writer) {
	fmt.Fprintln(w, "\n"+dim("→ This directory has no .git, so .git/config and .git/hooks are not scanned."))
	fmt.Fprintln(w, dim("→ Only the working tree files were checked. Run side-eye inside a clone for the git checks."))
}

// printCloneAdvice reminds the user of the safe way to get a working tree.
func printCloneAdvice(w io.Writer, t *remoteTarget) {
	fmt.Fprintln(w, "\n"+dim("→ A remote scan cannot read .git/config or .git/hooks. They exist only after a clone."))
	fmt.Fprintln(w, dim("→ Clone without a working tree, scan locally, then check out:"))
	printCommand(w, "git clone --no-checkout %s %s", t.clone, t.repo)
	printCommand(w, "side-eye %s", t.repo)
	printCommand(w, "git -C %s checkout", t.repo)
}

func location(root string, f Finding) string {
	path := f.Path
	if filepath.IsAbs(f.Path) {
		if rel, err := filepath.Rel(root, f.Path); err == nil && !strings.HasPrefix(rel, "..") {
			path = rel
		}
	}
	if f.Line > 0 {
		return fmt.Sprintf("%s:%d", path, f.Line)
	}
	return path
}

// severityAction gives the reader one clear next step for a finding.
func severityAction(s Severity) string {
	switch s {
	case SeverityCritical:
		return "Do not clone, open, or run git here until you review this."
	case SeverityHigh:
		return "Review this before you open or install the project."
	case SeverityMedium:
		return "Check this before you run project commands."
	default:
		return "Worth a look before you trust the repo."
	}
}

func severityCounts(findings []Finding) string {
	counts := map[Severity]int{}
	for _, f := range findings {
		counts[f.Severity]++
	}
	var parts []string
	for s := SeverityCritical; s >= SeverityInfo; s-- {
		if counts[s] > 0 {
			parts = append(parts, severityStyle(s)(fmt.Sprintf("%d %s", counts[s], strings.ToLower(s.String()))))
		}
	}
	return strings.Join(parts, ", ")
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}
