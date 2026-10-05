package scan

import "strings"

// Severity ranks how dangerous a finding is. Higher is worse.
type Severity int

const (
	SeverityInfo Severity = iota
	SeverityLow
	SeverityMedium
	SeverityHigh
	SeverityCritical
)

func (s Severity) String() string {
	switch s {
	case SeverityCritical:
		return "CRITICAL"
	case SeverityHigh:
		return "HIGH"
	case SeverityMedium:
		return "MEDIUM"
	case SeverityLow:
		return "LOW"
	default:
		return "INFO"
	}
}

func parseSeverity(s string) (Severity, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "info":
		return SeverityInfo, true
	case "low":
		return SeverityLow, true
	case "medium":
		return SeverityMedium, true
	case "high":
		return SeverityHigh, true
	case "critical":
		return SeverityCritical, true
	case "none":
		return SeverityCritical + 1, true
	}
	return SeverityInfo, false
}

// Finding is one risky thing the scanner found. Path is absolute; the report
// prints it relative to the scanned root.
type Finding struct {
	Severity Severity `json:"severity"`
	Path     string   `json:"path"`
	Line     int      `json:"line,omitempty"`
	Title    string   `json:"title"`
	Detail   string   `json:"detail,omitempty"`
}

func highestSeverity(findings []Finding) Severity {
	highest := SeverityInfo
	for _, f := range findings {
		if f.Severity > highest {
			highest = f.Severity
		}
	}
	return highest
}

// exitCodeFor turns the findings into a process exit code: 1 when the highest
// severity reaches the threshold, else 0.
func exitCodeFor(findings []Finding, threshold Severity) int {
	if highestSeverity(findings) >= threshold {
		return 1
	}
	return 0
}
