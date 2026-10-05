package scan

import (
	"flag"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestParseRemoteURL(t *testing.T) {
	cases := []struct {
		in    string
		host  string
		owner string
		repo  string
	}{
		{"https://github.com/owner/repo.git", "github.com", "owner", "repo"},
		{"https://github.com/owner/repo", "github.com", "owner", "repo"},
		{"git@github.com:owner/repo.git", "github.com", "owner", "repo"},
		{"ssh://git@github.com/owner/repo.git", "github.com", "owner", "repo"},
		{"github.com/owner/repo", "github.com", "owner", "repo"},
		{"owner/repo", "github.com", "owner", "repo"},
		{"https://gitlab.com/group/sub/repo.git", "gitlab.com", "group/sub", "repo"},
	}
	for _, c := range cases {
		got, err := parseRemoteURL(c.in, "", "")
		if err != nil {
			t.Errorf("parseRemoteURL(%q): %v", c.in, err)
			continue
		}
		if got.host != c.host || got.owner != c.owner || got.repo != c.repo {
			t.Errorf("parseRemoteURL(%q) = %s/%s/%s, want %s/%s/%s",
				c.in, got.host, got.owner, got.repo, c.host, c.owner, c.repo)
		}
	}
}

func TestIsRemoteArg(t *testing.T) {
	remote := []string{
		"https://github.com/owner/repo",
		"git@github.com:owner/repo.git",
		"github.com/owner/repo",
		"owner/repo",
	}
	for _, arg := range remote {
		if !isRemoteArg(arg) {
			t.Errorf("isRemoteArg(%q) = false, want true", arg)
		}
	}

	local := t.TempDir()
	if isRemoteArg(local) {
		t.Errorf("isRemoteArg(%q) = true, want false", local)
	}
	if isRemoteArg(filepath.Join(local, "repo")) {
		t.Errorf("isRemoteArg(local subpath) = true, want false")
	}
}

type fakeSource struct {
	files map[string][]byte
	hooks []string
}

func (f fakeSource) file(p string) []byte { return f.files[p] }
func (f fakeSource) hookFiles() []string  { return f.hooks }

func (f fakeSource) reportPath(rel string) string { return remoteReportPath(rel) }

func (f fakeSource) list() []string {
	paths := make([]string, 0, len(f.files))
	for p := range f.files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

func TestScanRemoteSource(t *testing.T) {
	src := fakeSource{
		files: map[string][]byte{
			".gitattributes":     []byte("*.bin filter=evil\n"),
			"package.json":       []byte(`{"scripts":{"postinstall":"node x.js"}}`),
			".envrc":             []byte("export X=1\n"),
			".vscode/tasks.json": []byte(`{"tasks":[{"runOptions":{"runOn":"folderOpen"}}]}`),
		},
		hooks: []string{".husky/pre-commit"},
	}

	var findings []Finding
	scanRemoteSource(src, func(f Finding) { findings = append(findings, f) })

	for _, want := range []string{
		"Attribute binds filter=evil",
		"npm install script: postinstall",
		"direnv file present",
		"VS Code auto-run task",
		"Husky hooks directory present",
		"Tracked hook file: pre-commit",
	} {
		if !hasTitle(findings, want) {
			t.Errorf("missing finding %q in %+v", want, findings)
		}
	}
}

func TestScanRemoteSourceClean(t *testing.T) {
	src := fakeSource{files: map[string][]byte{}, hooks: nil}
	var findings []Finding
	scanRemoteSource(src, func(f Finding) { findings = append(findings, f) })
	if len(findings) != 0 {
		t.Errorf("want no findings, got %+v", findings)
	}
}

func TestReadFileMissing(t *testing.T) {
	if readFile(filepath.Join(t.TempDir(), "nope")) != nil {
		t.Error("readFile on missing file should return nil")
	}
	data := []byte("x")
	p := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if string(readFile(p)) != "x" {
		t.Error("readFile returned wrong content")
	}
}

// TestResolveTokenPrefersTheEnvironment checks a token reaches the scan without
// ever passing through argv, where ps and shell history would expose it.
func TestResolveTokenPrefersTheEnvironment(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "from-github-token")
	t.Setenv("GH_TOKEN", "from-gh-token")

	got, err := resolveToken("")
	if err != nil {
		t.Fatalf("resolveToken: %v", err)
	}
	if got != "from-github-token" {
		t.Errorf("token = %q, want GITHUB_TOKEN to win", got)
	}

	t.Setenv("GITHUB_TOKEN", "")
	got, err = resolveToken("")
	if err != nil {
		t.Fatalf("resolveToken: %v", err)
	}
	if got != "from-gh-token" {
		t.Errorf("token = %q, want the GH_TOKEN fallback", got)
	}

	t.Setenv("GH_TOKEN", "  ")
	got, err = resolveToken("")
	if err != nil {
		t.Fatalf("resolveToken: %v", err)
	}
	if got != "" {
		t.Errorf("token = %q, want empty for a whitespace-only value", got)
	}
}

func TestResolveTokenFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	// A token file usually ends with a newline; it must not become part of the
	// header value.
	writeFile(t, path, "ghp_fromfile\n")

	got, err := resolveToken(path)
	if err != nil {
		t.Fatalf("resolveToken: %v", err)
	}
	if got != "ghp_fromfile" {
		t.Errorf("token = %q, want %q", got, "ghp_fromfile")
	}

	if _, err := resolveToken(filepath.Join(dir, "missing")); err == nil {
		t.Error("resolveToken accepted a missing -token-file")
	}
}

// TestNoTokenFlagExists is the guard for the leak itself: the flag is gone, so
// there is no way to hand a secret to the process through argv.
func TestNoTokenFlagExists(t *testing.T) {
	fs := flag.NewFlagSet("side-eye", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerScanFlags(fs)

	if f := fs.Lookup("token"); f != nil {
		t.Errorf("-token still exists, so a secret can be read from ps: %q", f.Usage)
	}
	if fs.Lookup("token-file") == nil {
		t.Error("-token-file must exist as the file-based replacement")
	}
}
