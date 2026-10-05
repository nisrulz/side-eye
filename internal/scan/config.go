package scan

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// gitConfigEntry is one key/value line in a git config file. Git config files
// also describe .gitmodules, so this type serves both.
type gitConfigEntry struct {
	Path       string
	Section    string
	Subsection string
	Key        string
	Value      string
	Line       int
}

// parseGitConfig reads a git config file without running git. It understands
// section headers, quoted subsections, comments, and backslash continuations.
func parseGitConfig(path string) ([]gitConfigEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseGitConfigReader(f, path)
}

func parseGitConfigBytes(data []byte, path string) ([]gitConfigEntry, error) {
	return parseGitConfigReader(strings.NewReader(string(data)), path)
}

func parseGitConfigReader(r io.Reader, path string) ([]gitConfigEntry, error) {
	var entries []gitConfigEntry
	var section, subsection string
	var cont string
	contLine := 0

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		text := stripComment(strings.TrimRight(scanner.Text(), "\r"))
		trimmed := strings.TrimSpace(text)
		if cont == "" {
			contLine = lineNo
		}
		if strings.HasSuffix(trimmed, "\\") {
			cont += strings.TrimSuffix(trimmed, "\\")
			continue
		}
		line := strings.TrimSpace(cont + trimmed)
		cont = ""
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			section, subsection = parseSectionHeader(line)
			continue
		}
		key, value := splitKeyValue(line)
		if key == "" {
			continue
		}
		entries = append(entries, gitConfigEntry{
			Path:       path,
			Section:    section,
			Subsection: subsection,
			Key:        key,
			Value:      value,
			Line:       contLine,
		})
	}
	return entries, scanner.Err()
}

// parseGitConfigTree follows include.path and includeIf.*.path directives up to
// a fixed depth, so keys smuggled into an included file are still inspected.
func parseGitConfigTree(path string) ([]gitConfigEntry, error) {
	seen := map[string]bool{}
	var all []gitConfigEntry

	var walk func(string, int) error
	walk = func(p string, depth int) error {
		if depth > 5 {
			return nil
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			return err
		}
		if seen[abs] {
			return nil
		}
		seen[abs] = true

		entries, err := parseGitConfig(abs)
		if err != nil {
			return err
		}
		all = append(all, entries...)

		dir := filepath.Dir(abs)
		for _, e := range entries {
			if !isIncludeEntry(e) {
				continue
			}
			inc := e.Value
			if !filepath.IsAbs(inc) {
				inc = filepath.Join(dir, inc)
			}
			if err := walk(inc, depth+1); err != nil {
				continue
			}
		}
		return nil
	}

	err := walk(path, 0)
	return all, err
}

func isIncludeEntry(e gitConfigEntry) bool {
	section := strings.ToLower(e.Section)
	return (section == "include" || strings.HasPrefix(section, "includeif")) &&
		strings.EqualFold(e.Key, "path")
}

func parseSectionHeader(s string) (string, string) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, ' '); i >= 0 {
		return s[:i], unquote(strings.TrimSpace(s[i+1:]))
	}
	return s, ""
}

func splitKeyValue(s string) (string, string) {
	inQuote := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\':
			i++
		case '"':
			inQuote = !inQuote
		case '=':
			if !inQuote {
				return strings.TrimSpace(s[:i]), unquote(strings.TrimSpace(s[i+1:]))
			}
		}
	}
	return strings.TrimSpace(s), "true"
}

func stripComment(s string) string {
	inQuote := false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"':
			inQuote = !inQuote
		case '#', ';':
			if !inQuote {
				return s[:i]
			}
		}
	}
	return s
}

func unquote(s string) string {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return s
	}
	s = s[1 : len(s)-1]
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			switch s[i] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			default:
				b.WriteByte(s[i])
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
