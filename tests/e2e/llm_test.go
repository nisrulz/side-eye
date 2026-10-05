package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// runSideEyeEnv runs the binary with extra environment variables, so a test can
// point the LLM pass at a local endpoint.
func runSideEyeEnv(t *testing.T, env []string, args ...string) result {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	exit := 0
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("run side-eye %v: %v", args, err)
		}
		exit = ee.ExitCode()
	}
	return result{exit: exit, stdout: stdout.String(), stderr: stderr.String()}
}

// openAIServer serves one OpenAI-compatible chat reply and counts the requests.
func openAIServer(t *testing.T, content string, hits *int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(hits, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"role": "assistant", "content": content}},
			},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestE2ELLMReportedFinding checks a model finding lands in the report and
// drives the exit code like any other finding.
func TestE2ELLMReportedFinding(t *testing.T) {
	root := makeRepo(t, "[core]\n\trepositoryformatversion = 0\n")
	writeFile(t, filepath.Join(root, "package.json"), `{"scripts":{"postinstall":"node x.js"}}`)

	var hits int32
	srv := openAIServer(t,
		`{"findings":[{"severity":"critical","path":"package.json","title":"postinstall pipes curl to sh","detail":"runs on npm install"}]}`,
		&hits)

	res := runSideEyeEnv(t, []string{
		"SIDE_EYE_LLM_URL=" + srv.URL + "/v1",
		"SIDE_EYE_LLM_MODEL=test-model",
	}, "-json", "-llm", root)

	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1\nstderr: %s\nstdout: %s", res.exit, res.stderr, res.stdout)
	}
	findings := scanJSON(t, res.stdout)
	if !hasTitle(findings, "LLM: postinstall pipes curl to sh") {
		t.Errorf("missing LLM finding, have %v", titles(findings))
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Errorf("endpoint hits = %d, want 1", hits)
	}
	if !strings.Contains(res.stderr, "LLM review read") {
		t.Errorf("stderr = %q", res.stderr)
	}
}

// TestE2ELLMRequiresConfig checks that -llm without a URL or model is a usage
// error, not a silent skip.
func TestE2ELLMRequiresConfig(t *testing.T) {
	root := makeRepo(t, "[core]\n\trepositoryformatversion = 0\n")
	res := runSideEyeEnv(t, []string{"SIDE_EYE_LLM_URL=", "SIDE_EYE_LLM_MODEL="}, "-llm", root)
	if res.exit != 2 {
		t.Errorf("exit = %d, want 2", res.exit)
	}
	if !strings.Contains(res.stderr, "SIDE_EYE_LLM_URL") {
		t.Errorf("stderr = %q", res.stderr)
	}
}

// TestE2ELLMOffByDefault checks that a configured endpoint is not called when
// the user does not pass -llm.
func TestE2ELLMOffByDefault(t *testing.T) {
	root := makeRepo(t, "[core]\n\trepositoryformatversion = 0\n")

	var hits int32
	srv := openAIServer(t, `{"findings":[]}`, &hits)

	res := runSideEyeEnv(t, []string{
		"SIDE_EYE_LLM_URL=" + srv.URL,
		"SIDE_EYE_LLM_MODEL=test-model",
	}, root)
	if res.exit != 0 {
		t.Errorf("exit = %d, want 0\nstderr: %s", res.exit, res.stderr)
	}
	if atomic.LoadInt32(&hits) != 0 {
		t.Error("endpoint was called without -llm")
	}
}

// TestE2ELLMPromptFileOverride checks SIDE_EYE_LLM_PROMPT_FILE replaces the
// general system prompt.
func TestE2ELLMPromptFileOverride(t *testing.T) {
	root := makeRepo(t, "[core]\n\trepositoryformatversion = 0\n")
	writeFile(t, filepath.Join(root, "package.json"), "{}")

	promptPath := filepath.Join(t.TempDir(), "prompt.txt")
	writeFile(t, promptPath, "CUSTOM_SYSTEM_PROMPT_MARKER")

	got := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if len(req.Messages) > 0 {
			got <- req.Messages[0].Content
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"role": "assistant", "content": `{"findings":[]}`}},
			},
		})
	}))
	t.Cleanup(srv.Close)

	res := runSideEyeEnv(t, []string{
		"SIDE_EYE_LLM_URL=" + srv.URL,
		"SIDE_EYE_LLM_MODEL=test-model",
		"SIDE_EYE_LLM_PROMPT_FILE=" + promptPath,
	}, "-json", "-llm", root)
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstderr: %s", res.exit, res.stderr)
	}
	select {
	case content := <-got:
		if !strings.HasPrefix(content, "CUSTOM_SYSTEM_PROMPT_MARKER") {
			t.Errorf("system content = %q", content)
		}
	default:
		t.Fatal("endpoint was not called")
	}
}

// TestE2ELLMFailureIsNonFatal checks a broken endpoint still prints the
// deterministic findings and does not change the exit code.
func TestE2ELLMFailureIsNonFatal(t *testing.T) {
	root := makeRepo(t, "[core]\n\trepositoryformatversion = 0\n")
	writeFile(t, filepath.Join(root, "package.json"), `{"scripts":{"postinstall":"node x.js"}}`)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	res := runSideEyeEnv(t, []string{
		"SIDE_EYE_LLM_URL=" + srv.URL,
		"SIDE_EYE_LLM_MODEL=test-model",
	}, "-json", "-llm", root)

	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstderr: %s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stderr, "LLM review skipped") {
		t.Errorf("stderr = %q", res.stderr)
	}
	if !strings.Contains(res.stderr, "checks above ran without it") {
		t.Errorf("stderr = %q, want it to say the scan still finished", res.stderr)
	}
	findings := scanJSON(t, res.stdout)
	if !hasTitle(findings, "npm install script: postinstall") {
		t.Errorf("deterministic findings lost, have %v", titles(findings))
	}
}
