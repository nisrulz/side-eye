package scan

import (
	"encoding/json"
	"fmt"
	"strings"
)

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

// MarshalJSON writes the severity as its name, not its number. The names are
// the contract: a consumer reading "severity": 3 has to hard-code the same
// order this file happens to use, and reordering the constants would silently
// change what every finding means.
func (s Severity) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

// UnmarshalJSON reads a severity name back, so -json output round-trips and a
// test can assert on the same shape the CLI emits.
func (s *Severity) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err != nil {
		return fmt.Errorf("severity must be a string: %w", err)
	}
	parsed, ok := parseSeverity(name)
	if !ok {
		return fmt.Errorf("unknown severity %q", name)
	}
	*s = parsed
	return nil
}

// parseSeverity reads a severity name. `none` is the -fail-on value that sits
// above critical, so it has no severity of its own.
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
	}
	return SeverityInfo, false
}

// parseFailOn reads the -fail-on value, where `none` means never fail.
func parseFailOn(s string) (Severity, bool) {
	if strings.EqualFold(strings.TrimSpace(s), "none") {
		return SeverityCritical + 1, true
	}
	return parseSeverity(s)
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
