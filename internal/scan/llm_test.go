package scan

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadLLMConfig(t *testing.T) {
	t.Setenv(llmEnvURL, "http://localhost:11434/v1")
	t.Setenv(llmEnvModel, "llama3.1")
	t.Setenv(llmEnvToken, "")
	cfg, err := loadLLMConfig()
	if err != nil {
		t.Fatalf("loadLLMConfig: %v", err)
	}
	if cfg.baseURL != "http://localhost:11434/v1" || cfg.model != "llama3.1" || cfg.token != "" {
		t.Errorf("cfg = %+v", cfg)
	}

	t.Setenv(llmEnvModel, "")
	if _, err := loadLLMConfig(); err == nil || !strings.Contains(err.Error(), llmEnvModel) {
		t.Errorf("missing model error = %v", err)
	}

	t.Setenv(llmEnvURL, "")
	t.Setenv(llmEnvModel, "llama3.1")
	if _, err := loadLLMConfig(); err == nil || !strings.Contains(err.Error(), llmEnvURL) {
		t.Errorf("missing url error = %v", err)
	}
}

func TestLLMChatURL(t *testing.T) {
	cases := map[string]string{
		"http://localhost:11434/v1":                   "http://localhost:11434/v1/chat/completions",
		"https://api.example.com/v1/":                 "https://api.example.com/v1/chat/completions",
		"https://api.example.com/v1/chat/completions": "https://api.example.com/v1/chat/completions",
	}
	for in, want := range cases {
		if got := (llmConfig{baseURL: in}).chatURL(); got != want {
			t.Errorf("chatURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildLLMPrompt(t *testing.T) {
	got := buildLLMPrompt([]string{"a.txt", "b.txt"}, []llmFile{{Path: "a.txt", Content: "hello"}})
	for _, want := range []string{"a.txt", "b.txt", `<file path="a.txt">`, "hello", "</file>"} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt missing %q:\n%s", want, got)
		}
	}
}

func TestParseLLMFindings(t *testing.T) {
	fenced := "Here you go:\n```json\n" +
		`{"findings":[{"severity":"critical","path":"a.sh","line":3,"title":"runs curl","detail":"hook"}]}` +
		"\n```\n"
	findings := parseLLMFindings(fenced)
	if len(findings) != 1 {
		t.Fatalf("findings = %+v", findings)
	}
	got := findings[0]
	if got.Title != "LLM: runs curl" || got.Severity != SeverityCritical || got.Path != "a.sh" || got.Line != 3 {
		t.Errorf("finding = %+v", got)
	}

	dropped := `{"findings":[` +
		`{"severity":"bogus","title":"x"},` +
		`{"severity":"none","title":"y"},` +
		`{"severity":"high","title":""},` +
		`{"severity":"low","title":"ok"}]}`
	if got := parseLLMFindings(dropped); len(got) != 1 || got[0].Title != "LLM: ok" {
		t.Errorf("dropped findings = %+v", got)
	}

	if got := parseLLMFindings("no json here"); got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestLLMContent(t *testing.T) {
	payload := []byte(`{"choices":[{"message":{"role":"assistant","content":"hi"}}]}`)
	content, err := llmContent(payload)
	if err != nil || content != "hi" {
		t.Fatalf("llmContent = %q, %v", content, err)
	}
	if _, err := llmContent([]byte(`{"choices":[]}`)); err == nil {
		t.Error("empty choices should error")
	}
	if _, err := llmContent([]byte(`not json`)); err == nil {
		t.Error("non-json should error")
	}
}

func TestScanLLM(t *testing.T) {
	var gotAuth string
	var gotBody llmRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		writeChatReply(w, `{"findings":[{"severity":"high","path":".git/config","title":"ssh command"}]}`)
	}))
	defer srv.Close()

	cfg := llmConfig{baseURL: srv.URL + "/v1", token: "secret", model: "m"}
	var findings []Finding
	err := scanLLM(cfg, []string{".git/config"}, []llmFile{{Path: ".git/config", Content: "x"}},
		func(f Finding) { findings = append(findings, f) })
	if err != nil {
		t.Fatalf("scanLLM: %v", err)
	}
	if gotAuth != "Bearer secret" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotBody.Model != "m" || len(gotBody.Messages) != 2 || gotBody.Messages[0].Role != "system" {
		t.Errorf("request = %+v", gotBody)
	}
	if len(findings) != 1 || findings[0].Title != "LLM: ssh command" || findings[0].Severity != SeverityHigh {
		t.Errorf("findings = %+v", findings)
	}
}

func TestScanLLMNoToken(t *testing.T) {
	var auth []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Values("Authorization")
		writeChatReply(w, `{"findings":[]}`)
	}))
	defer srv.Close()

	cfg := llmConfig{baseURL: srv.URL, model: "m"}
	if err := scanLLM(cfg, nil, nil, func(Finding) {}); err != nil {
		t.Fatalf("scanLLM: %v", err)
	}
	if len(auth) != 0 {
		t.Errorf("Authorization header present with empty token: %v", auth)
	}
}

func TestScanLLMErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer srv.Close()

	cfg := llmConfig{baseURL: srv.URL, model: "m"}
	if err := scanLLM(cfg, nil, nil, func(Finding) {}); err == nil {
		t.Error("non-200 response should error")
	}
}

func writeChatReply(w http.ResponseWriter, content string) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"choices": []map[string]any{
			{"message": map[string]string{"role": "assistant", "content": content}},
		},
	})
}

func TestGeneralLLMPromptDefault(t *testing.T) {
	t.Setenv(llmEnvPromptFile, "")
	got := generalLLMPrompt()
	// The prompt must ask for attack paths, rule out the ordinary code every
	// project has, and say that nothing is executed. Without the last one the
	// model reports build commands as if they were the tool running them.
	for _, want := range []string{
		"You review repositories for side-eye",
		"Do not report",
		"Application code doing its job",
		"never runs git, a build, a hook, or any script",
		`{"findings":[]}`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("default prompt missing %q\ngot: %q", want, got)
		}
	}
}

func TestGeneralLLMPromptFileOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prompt.txt")
	writeFile(t, path, "Only report hook findings.\n")
	t.Setenv(llmEnvPromptFile, path)
	if got := generalLLMPrompt(); got != "Only report hook findings." {
		t.Errorf("override prompt = %q", got)
	}
}

func TestGeneralLLMPromptEmptyFileFallsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prompt.txt")
	writeFile(t, path, "   \n")
	t.Setenv(llmEnvPromptFile, path)
	if got := generalLLMPrompt(); !strings.Contains(got, "You review repositories for side-eye") {
		t.Errorf("empty file should fall back, got %q", got)
	}
}

func TestGeneralLLMPromptMissingFileFallsBack(t *testing.T) {
	t.Setenv(llmEnvPromptFile, filepath.Join(t.TempDir(), "nope.txt"))
	if got := generalLLMPrompt(); !strings.Contains(got, "You review repositories for side-eye") {
		t.Errorf("missing file should fall back, got %q", got)
	}
}

func TestLLMSystemPromptAppendsResponse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prompt.txt")
	writeFile(t, path, "Be terse.")
	t.Setenv(llmEnvPromptFile, path)
	got := llmSystemPrompt()
	if !strings.HasPrefix(got, "Be terse.") {
		t.Errorf("system prompt prefix = %q", got)
	}
	for _, want := range []string{"Return only a JSON object", `"findings"`, "critical, high, medium, low, or info"} {
		if !strings.Contains(got, want) {
			t.Errorf("system prompt missing %q: %q", want, got)
		}
	}
}

func TestScanLLMUsesPromptOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prompt.txt")
	writeFile(t, path, "Custom general prompt.")
	t.Setenv(llmEnvPromptFile, path)

	var gotBody llmRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request: %v", err)
		}
		writeChatReply(w, `{"findings":[]}`)
	}))
	defer srv.Close()

	cfg := llmConfig{baseURL: srv.URL, model: "m"}
	if err := scanLLM(cfg, nil, nil, func(Finding) {}); err != nil {
		t.Fatalf("scanLLM: %v", err)
	}
	if len(gotBody.Messages) == 0 || !strings.HasPrefix(gotBody.Messages[0].Content, "Custom general prompt.") {
		t.Errorf("system message = %+v", gotBody.Messages)
	}
}
