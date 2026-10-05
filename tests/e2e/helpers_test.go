package e2e

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// bin is the built side-eye binary. TestMain builds it once, so each test runs
// the real CLI as a black box.
var bin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "side-eye-e2e-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e: create temp dir:", err)
		os.Exit(1)
	}
	bin = filepath.Join(dir, "side-eye")

	root, err := filepath.Abs("../..")
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e: resolve repo root:", err)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	build := exec.Command("go", "build", "-buildvcs=false", "-o", bin, "./cmd/side-eye")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "e2e: go build failed: %v\n%s", err, out)
		os.RemoveAll(dir)
		os.Exit(1)
	}

	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type finding struct {
	Severity int    `json:"severity"`
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Title    string `json:"title"`
	Detail   string `json:"detail"`
}

type result struct {
	exit   int
	stdout string
	stderr string
}

func runSideEye(t *testing.T, args ...string) result {
	t.Helper()
	cmd := exec.Command(bin, args...)
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

func scanJSON(t *testing.T, stdout string) []finding {
	t.Helper()
	var doc struct {
		Findings []finding `json:"findings"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("parse json output: %v\n%s", err, stdout)
	}
	return doc.Findings
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func makeRepo(t *testing.T, config string) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeFile(t, filepath.Join(root, ".git", "config"), config)
	return root
}

func writeAll(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		writeFile(t, filepath.Join(root, name), body)
	}
}

func writeZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func assertTitles(t *testing.T, findings []finding, wants []string) {
	t.Helper()
	for _, want := range wants {
		if !hasTitle(findings, want) {
			t.Errorf("missing finding %q\nhave: %v", want, titles(findings))
		}
	}
}

func titles(findings []finding) []string {
	got := make([]string, 0, len(findings))
	for _, f := range findings {
		got = append(got, f.Title)
	}
	return got
}

func hasTitle(findings []finding, title string) bool {
	for _, f := range findings {
		if f.Title == title {
			return true
		}
	}
	return false
}
