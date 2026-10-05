package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestE2EWorktreeGitFile checks a linked worktree or submodule, where .git is a
// file that points at the real git directory. The config and hooks live in that
// directory, not next to the worktree, so a scanner that only reads .git/ would
// miss them.
func TestE2EWorktreeGitFile(t *testing.T) {
	root := t.TempDir()
	gitDir := t.TempDir()
	writeFile(t, filepath.Join(gitDir, "HEAD"), "ref: refs/heads/main\n")
	writeFile(t, filepath.Join(gitDir, "config"), "[core]\n\thooksPath = .githooks\n")
	writeFile(t, filepath.Join(root, ".git"), "gitdir: "+gitDir+"\n")
	writeFile(t, filepath.Join(root, ".githooks", "post-checkout"), "#!/bin/sh\nsh .payload\n")
	writeFile(t, filepath.Join(root, "package.json"), `{"scripts":{"postinstall":"node x.js"}}`)

	res := runSideEye(t, "-json", root)
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1\nstderr: %s", res.exit, res.stderr)
	}
	assertTitles(t, scanJSON(t, res.stdout), []string{
		"core.hooksPath",
		"Active git hook: post-checkout",
		"npm install script: postinstall",
	})
}

// TestE2EBareRepo checks a bare repository, where HEAD and config sit at the
// root. A scan must read config and hooks without a working tree.
func TestE2EBareRepo(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "HEAD"), "ref: refs/heads/main\n")
	writeFile(t, filepath.Join(root, "config"), "[filter \"evil\"]\n\tsmudge = sh -c x\n")
	writeFile(t, filepath.Join(root, "hooks", "post-checkout"), "#!/bin/sh\n")

	res := runSideEye(t, "-json", root)
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1\nstderr: %s", res.exit, res.stderr)
	}
	assertTitles(t, scanJSON(t, res.stdout), []string{
		"filter.evil.smudge",
		"Active git hook: post-checkout",
	})
}

// TestE2EHookVariants checks the ways a hook can hide: a symlink to another
// binary, a file with no execute bit (it still runs on Windows and after
// chmod), uncommon hook names, and the .sample files git ships but never runs.
func TestE2EHookVariants(t *testing.T) {
	root := makeRepo(t, "[core]\n\trepositoryformatversion = 0\n")
	writeFile(t, filepath.Join(root, ".git", "hooks", "pre-commit"), "#!/bin/sh\n")
	if err := os.Symlink("/bin/sh", filepath.Join(root, ".git", "hooks", "post-checkout")); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	writeFile(t, filepath.Join(root, ".git", "hooks", "fsmonitor-watchman"), "#!/bin/sh\n")
	writeFile(t, filepath.Join(root, ".git", "hooks", "reference-transaction"), "#!/bin/sh\n")
	writeFile(t, filepath.Join(root, ".git", "hooks", "pre-commit.sample"), "ignored\n")

	res := runSideEye(t, "-json", root)
	findings := scanJSON(t, res.stdout)
	assertTitles(t, findings, []string{
		"Active git hook: pre-commit",
		"Active git hook: post-checkout",
		"Active git hook: fsmonitor-watchman",
		"Active git hook: reference-transaction",
	})
	if hasTitle(findings, "Active git hook: pre-commit.sample") {
		t.Error("sample hooks must be ignored")
	}
	for _, f := range findings {
		if f.Title == "Active git hook: post-checkout" && !strings.Contains(f.Detail, "symlink") {
			t.Errorf("symlink hook detail = %q", f.Detail)
		}
		if f.Title == "Active git hook: pre-commit" && !strings.Contains(f.Detail, "not executable") {
			t.Errorf("non-exec hook detail = %q", f.Detail)
		}
	}
}

// TestE2EConfigNegatives checks entries that look risky but are safe:
// fsmonitor=true uses the built-in monitor, an alias without ! runs no shell,
// a built-in credential helper is trusted, and only the ext and file protocols
// are flagged.
func TestE2EConfigNegatives(t *testing.T) {
	root := makeRepo(t, `[core]
	fsmonitor = true
[alias]
	safe = status
[credential]
	helper = cache
[protocol "https"]
	allow = always
`)
	res := runSideEye(t, "-json", root)
	if got := scanJSON(t, res.stdout); len(got) != 0 {
		t.Errorf("expected no findings, got %v", titles(got))
	}
}

// TestE2EIncludeChain checks config hidden behind include.path and includeIf. A
// scam can split its payload across included files, including nested ones, so
// the scanner must follow the chain.
func TestE2EIncludeChain(t *testing.T) {
	root := makeRepo(t, `[include]
	path = a.cfg
[includeIf "gitdir:~/x/"]
	path = c.cfg
`)
	writeFile(t, filepath.Join(root, ".git", "a.cfg"), "[include]\n\tpath = b.cfg\n")
	writeFile(t, filepath.Join(root, ".git", "b.cfg"), "[alias]\n\tnested = !echo hi\n")
	writeFile(t, filepath.Join(root, ".git", "c.cfg"), "[alias]\n\tconditional = !echo hi\n")

	res := runSideEye(t, "-json", root)
	assertTitles(t, scanJSON(t, res.stdout), []string{
		"include.path",
		"includeif.path",
		"alias.nested",
		"alias.conditional",
	})
}

// TestE2EConfigCaseInsensitive checks mixed-case sections and keys. Git keys are
// case-insensitive, so an attacker can spell hooksPath or smudge differently to
// dodge a naive string match.
func TestE2EConfigCaseInsensitive(t *testing.T) {
	root := makeRepo(t, `[Core]
	HooksPath = .githooks
[FILTER "Evil"]
	SMUDGE = sh -c x
`)
	writeFile(t, filepath.Join(root, ".githooks", "post-checkout"), "#!/bin/sh\n")

	res := runSideEye(t, "-json", root)
	assertTitles(t, scanJSON(t, res.stdout), []string{
		"core.hooksPath",
		"filter.evil.smudge",
		"Active git hook: post-checkout",
	})
}

// TestE2EAttributesNegatives checks attribute values that bind no external
// command: filter=false disables the filter, merge=text is built in, and an
// empty diff value sets nothing.
func TestE2EAttributesNegatives(t *testing.T) {
	root := makeRepo(t, "[core]\n\trepositoryformatversion = 0\n")
	writeFile(t, filepath.Join(root, ".gitattributes"), "*.txt filter=false\n*.txt merge=text\n*.txt diff=\n")

	res := runSideEye(t, "-json", root)
	if got := scanJSON(t, res.stdout); len(got) != 0 {
		t.Errorf("expected no findings, got %v", titles(got))
	}
}

// TestE2EGitmodulesAndScripts checks submodule tricks and package manager
// install scripts. A leading dash can inject git options, update=! runs a shell
// on submodule update, and every install script runs on npm or yarn install.
func TestE2EGitmodulesAndScripts(t *testing.T) {
	root := makeRepo(t, "[core]\n\trepositoryformatversion = 0\n")
	writeFile(t, filepath.Join(root, ".gitmodules"),
		"[submodule \"dash\"]\n\turl = -oProxyCommand=evil\n[submodule \"u\"]\n\tupdate = !sh evil.sh\n")
	writeFile(t, filepath.Join(root, "package.json"),
		`{"scripts":{"preinstall":"a","install":"b","postinstall":"c","prepare":"d","prepublish":"e","prepublishOnly":"f"}}`)

	res := runSideEye(t, "-json", root)
	assertTitles(t, scanJSON(t, res.stdout), []string{
		"submodule url",
		"submodule.u.update",
		"npm install script: preinstall, install, postinstall, prepare, prepublish, prepublishOnly",
	})
}
