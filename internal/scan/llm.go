package scan

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Environment variables that configure the optional LLM review pass.
const (
	llmEnvURL        = "SIDE_EYE_LLM_URL"
	llmEnvToken      = "SIDE_EYE_LLM_TOKEN"
	llmEnvModel      = "SIDE_EYE_LLM_MODEL"
	llmEnvPromptFile = "SIDE_EYE_LLM_PROMPT_FILE"
	llmEnvTimeout    = "SIDE_EYE_LLM_TIMEOUT"
)

// defaultLLMTimeout bounds one review request. A local model reading the whole
// execution surface takes minutes, so the wait is generous and configurable.
const defaultLLMTimeout = 5 * time.Minute

// llmConfig holds the OpenAI-compatible endpoint settings.
type llmConfig struct {
	baseURL string
	token   string
	model   string
	timeout time.Duration
}

// loadLLMConfig reads the endpoint settings from the environment. The URL and
// the model are required; the token is optional, so Ollama works with no token.
func loadLLMConfig() (llmConfig, error) {
	cfg := llmConfig{
		baseURL: strings.TrimRight(strings.TrimSpace(os.Getenv(llmEnvURL)), "/"),
		token:   strings.TrimSpace(os.Getenv(llmEnvToken)),
		model:   strings.TrimSpace(os.Getenv(llmEnvModel)),
		timeout: llmWait(),
	}
	var missing []string
	if cfg.baseURL == "" {
		missing = append(missing, llmEnvURL)
	}
	if cfg.model == "" {
		missing = append(missing, llmEnvModel)
	}
	if len(missing) > 0 {
		return cfg, fmt.Errorf("-llm needs %s (token is optional)", strings.Join(missing, " and "))
	}
	return cfg, nil
}

// llmWait reads SIDE_EYE_LLM_TIMEOUT as a Go duration, such as 90s or 10m. An
// empty or unusable value keeps the default, because a scan must still run.
func llmWait() time.Duration {
	raw := strings.TrimSpace(os.Getenv(llmEnvTimeout))
	if raw == "" {
		return defaultLLMTimeout
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return defaultLLMTimeout
	}
	return d
}

// chatURL accepts either a base URL or the full completions URL.
func (c llmConfig) chatURL() string {
	base := strings.TrimRight(c.baseURL, "/")
	if strings.HasSuffix(base, "/chat/completions") {
		return base
	}
	return base + "/chat/completions"
}

// llmFile is one file the model reads. Content is empty for a listing-only entry.
type llmFile struct {
	Path    string
	Content string
}

type llmMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type llmRequest struct {
	Model       string       `json:"model"`
	Messages    []llmMessage `json:"messages"`
	Temperature float64      `json:"temperature"`
}

type llmChatResponse struct {
	Choices []struct {
		Message llmMessage `json:"message"`
	} `json:"choices"`
}

type llmFindingDoc struct {
	Findings []struct {
		Severity string `json:"severity"`
		Path     string `json:"path"`
		Line     int    `json:"line"`
		Title    string `json:"title"`
		Detail   string `json:"detail"`
	} `json:"findings"`
}

//go:embed prompt.txt
var defaultLLMPrompt string

// llmResponsePrompt is always appended to the general prompt so the reply stays
// machine-readable.
const llmResponsePrompt = "Return only a JSON object, with no prose and no code fences, in this shape: " +
	`{"findings":[{"severity":"critical","path":"relative/path","line":0,"title":"what runs","detail":"what runs and when"}]}. ` +
	"Use severity critical, high, medium, low, or info. Use line 0 when no line applies. " +
	"Report only findings you can point to in the supplied files, and only code that runs something. " +
	"An empty list is the correct answer for an ordinary project, so do not report a file for existing. " +
	`If you find nothing, return {"findings":[]}.`

// generalLLMPrompt returns the prompt from SIDE_EYE_LLM_PROMPT_FILE when that
// file has content, else the embedded default. The override is opt-in, so a
// file inside the scanned repository cannot hijack the system prompt.
func generalLLMPrompt() string {
	path := strings.TrimSpace(os.Getenv(llmEnvPromptFile))
	if path == "" {
		return strings.TrimSpace(defaultLLMPrompt)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return strings.TrimSpace(defaultLLMPrompt)
	}
	if prompt := strings.TrimSpace(string(data)); prompt != "" {
		return prompt
	}
	return strings.TrimSpace(defaultLLMPrompt)
}

// llmSystemPrompt is the general prompt plus the fixed response contract.
func llmSystemPrompt() string {
	return generalLLMPrompt() + " " + llmResponsePrompt
}

// scanLLM sends the repository surface to the model and adds each finding it
// reports. A failure returns an error so the caller can warn and keep the
// deterministic results.
func scanLLM(cfg llmConfig, listing []string, files []llmFile, add func(Finding)) error {
	answer, err := cfg.review(listing, files)
	if err != nil {
		return err
	}
	for _, f := range parseLLMFindings(answer) {
		add(f)
	}
	return nil
}

// review sends one chat request and returns the assistant message text.
func (c llmConfig) review(listing []string, files []llmFile) (string, error) {
	req, err := c.newChatRequest(listing, files)
	if err != nil {
		return "", err
	}

	resp, err := (&http.Client{Timeout: c.wait()}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxReadBytes))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("endpoint returned %s", resp.Status)
	}
	return llmContent(payload)
}

// wait is the per-request timeout. A config built in a test without loadLLMConfig
// still gets the default, so no caller can end up with no timeout at all.
func (c llmConfig) wait() time.Duration {
	if c.timeout <= 0 {
		return defaultLLMTimeout
	}
	return c.timeout
}

// newChatRequest builds the OpenAI-compatible request that carries the general
// prompt, the response contract, and the repository surface.
func (c llmConfig) newChatRequest(listing []string, files []llmFile) (*http.Request, error) {
	body, err := json.Marshal(llmRequest{
		Model: c.model,
		Messages: []llmMessage{
			{Role: "system", Content: llmSystemPrompt()},
			{Role: "user", Content: buildLLMPrompt(listing, files)},
		},
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, c.chatURL(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "side-eye")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return req, nil
}

// buildLLMPrompt renders the file listing and the file contents for the model.
func buildLLMPrompt(listing []string, files []llmFile) string {
	var b strings.Builder
	b.WriteString("Repository file listing:\n")
	for _, p := range listing {
		b.WriteString(p)
		b.WriteByte('\n')
	}
	b.WriteString("\nFiles with content:\n")
	for _, f := range files {
		fmt.Fprintf(&b, "<file path=%q>\n%s\n</file>\n", f.Path, f.Content)
	}
	return b.String()
}

// llmContent pulls the assistant message text from an OpenAI-compatible reply.
func llmContent(payload []byte) (string, error) {
	var doc llmChatResponse
	if err := json.Unmarshal(payload, &doc); err != nil {
		return "", fmt.Errorf("response was not JSON: %w", err)
	}
	if len(doc.Choices) == 0 {
		return "", fmt.Errorf("response had no choices")
	}
	return doc.Choices[0].Message.Content, nil
}

// vagueLLMFindingTitles name a category of code rather than a way to run code.
// A title like "Network communication" describes what an app is for, so it is
// noise on a repository that is doing its job. These are the titles observed
// from a well-known Android project.
var vagueLLMFindingTitles = []string{
	"network communication", "network call", "network request", "api call",
	"application manifest", "manifest configuration", "app manifest",
	"production build", "build execution", "ci/cd", "cicd",
	"code execution via", "execution via", "build configuration",
	"gradle build", "build script", "build process", "compile",
	"test framework", "logging", "error handling", "data handling",
	"data validation", "input validation", "configuration management",
	"dependency management", "version control", "static analysis",
}

// vagueLLMFinding reports if a title only names a category of code. The detail
// still has to say what runs and when, so a title that names a real mechanism
// is kept even when it is broad.
func vagueLLMFinding(title string) bool {
	lower := strings.ToLower(title)
	for _, vague := range vagueLLMFindingTitles {
		if lower == vague || strings.HasPrefix(lower, vague+":") {
			return true
		}
	}
	return false
}

// parseLLMFindings extracts findings from the model text. It tolerates code
// fences and prose around the JSON object, and drops entries it cannot use.
func parseLLMFindings(content string) []Finding {
	start := strings.IndexByte(content, '{')
	end := strings.LastIndexByte(content, '}')
	if start < 0 || end <= start {
		return nil
	}
	var doc llmFindingDoc
	if err := json.Unmarshal([]byte(content[start:end+1]), &doc); err != nil {
		return nil
	}
	var findings []Finding
	for _, f := range doc.Findings {
		title := strings.TrimSpace(f.Title)
		if title == "" || strings.EqualFold(strings.TrimSpace(f.Severity), "none") {
			continue
		}
		sev, ok := parseSeverity(f.Severity)
		if !ok || vagueLLMFinding(title) {
			continue
		}
		findings = append(findings, Finding{
			Severity: sev,
			Path:     strings.TrimSpace(f.Path),
			Line:     f.Line,
			Title:    "LLM: " + title,
			Detail:   strings.TrimSpace(f.Detail),
		})
	}
	return findings
}
