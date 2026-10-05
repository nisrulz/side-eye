package scan

import (
	"os"
	"strings"
)

// Generic readers shared by the checks.

// readFile returns the file contents, or nil when the file is missing or cannot
// be read, so every check treats an absent file the same way.
func readFile(path string) []byte {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return data
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func hasToken(data []byte, token string) bool {
	return strings.Contains(string(data), token)
}

// findWordTokens is findTokens with identifier boundaries. A Gradle build
// script names a configuration `prodReleaseRuntimeClasspath`, so a plain
// substring match on `classpath` reports every Android project. This matches
// `classpath` and `classpath(` but not `RuntimeClasspath`.
func findWordTokens(text string, tokens ...string) []string {
	var found []string
	for _, token := range tokens {
		if containsWord(text, token) {
			found = append(found, token)
		}
	}
	return found
}

// containsWord reports if token appears in text outside a longer identifier.
// Both text and token are expected to be lower case already, because the
// callers match case-insensitively.
//
// The token is matched literally, so `apply(` does not match `apply false`, and
// the boundaries are checked around its alphanumeric core so `classpath(` does
// not match prodReleaseRuntimeClasspath.
func containsWord(text, token string) bool {
	core := strings.TrimRight(token, "({ \t:")
	if core == "" {
		return strings.Contains(text, token)
	}
	for i := 0; i < len(text); {
		j := strings.Index(text[i:], token)
		if j < 0 {
			return false
		}
		start := i + j
		if !isIdentByte(byteBefore(text, start)) && !isIdentByte(byteAfter(text, start+len(core))) {
			return true
		}
		i = start + 1
	}
	return false
}

func byteBefore(text string, i int) byte {
	if i <= 0 {
		return 0
	}
	return text[i-1]
}

func byteAfter(text string, i int) byte {
	if i >= len(text) {
		return 0
	}
	return text[i]
}

func isIdentByte(b byte) bool {
	return b == '_' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}

func findTokens(data []byte, tokens ...string) []string {
	var found []string
	for _, token := range tokens {
		if hasToken(data, token) {
			found = append(found, token)
		}
	}
	return found
}

// stripGradleComments removes // and /* */ comments from a Gradle or Kotlin
// script. A quoted string is kept, so a URL inside one survives.
//
// A Gradle build script documents itself about classpaths, so matching the raw
// text reports every well-written project.
func stripGradleComments(data []byte) []byte {
	var b strings.Builder
	for i := 0; i < len(data); {
		switch {
		case data[i] == '"' && i+2 < len(data) && data[i+1] == '"' && data[i+2] == '"':
			end := strings.Index(string(data[i+3:]), `"""`)
			if end < 0 {
				return []byte(b.String())
			}
			b.Write(data[i : i+3+end+3])
			i += 3 + end + 3
		case data[i] == '"':
			b.WriteByte(data[i])
			i++
			for i < len(data) {
				if data[i] == '\\' && i+1 < len(data) {
					b.Write(data[i : i+2])
					i += 2
					continue
				}
				b.WriteByte(data[i])
				closed := data[i] == '"'
				i++
				if closed {
					break
				}
			}
		case data[i] == '/' && i+1 < len(data) && data[i+1] == '/':
			for i < len(data) && data[i] != '\n' {
				i++
			}
		case data[i] == '/' && i+1 < len(data) && data[i+1] == '*':
			i += 2
			for i+1 < len(data) && !(data[i] == '*' && data[i+1] == '/') {
				i++
			}
			i += 2
		default:
			b.WriteByte(data[i])
			i++
		}
	}
	return []byte(b.String())
}

// stripJSONComments removes line and block comments outside strings, because the
// JSON files this tool reads may come from editors and package managers that
// write comments.
func stripJSONComments(data []byte) []byte {
	var b strings.Builder
	inString := false
	for i := 0; i < len(data); i++ {
		c := data[i]
		if inString {
			b.WriteByte(c)
			if c == '\\' && i+1 < len(data) {
				i++
				b.WriteByte(data[i])
			} else if c == '"' {
				inString = false
			}
			continue
		}
		switch {
		case c == '"':
			inString = true
			b.WriteByte(c)
		case c == '/' && i+1 < len(data) && data[i+1] == '/':
			for i < len(data) && data[i] != '\n' {
				i++
			}
			b.WriteByte('\n')
		case c == '/' && i+1 < len(data) && data[i+1] == '*':
			i += 2
			for i+1 < len(data) && !(data[i] == '*' && data[i+1] == '/') {
				i++
			}
			i++
		default:
			b.WriteByte(c)
		}
	}
	return []byte(b.String())
}
