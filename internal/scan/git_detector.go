package scan

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// scanGit covers everything git itself runs: config keys, hooks, and the files
// that bind a path to a filter, diff, or merge driver.
//
// The config and hook-directory checks need a git directory. When the target is
// a plain directory they are skipped, but the tracked hook directories and
// .gitattributes still apply, because git reads them the moment the directory
// becomes a repository.
func scanGit(repo *repoLayout, add func(Finding)) error {
	if repo.plain {
		scanPlainGitSurface(repo, add)
		return nil
	}

	configPath := filepath.Join(repo.gitDir, "config")
	entries, err := parseGitConfigTree(configPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", configPath, err)
	}

	evalConfig(entries, add)
	scanHooks(repo.hooksDir(entries), add)
	scanGitAttributes(repo, add)
	checkGitmodules(readFile(filepath.Join(repo.root, ".gitmodules")), ".gitmodules", add)
	return nil
}

// scanPlainGitSurface runs the git checks that work without a .git directory.
func scanPlainGitSurface(repo *repoLayout, add func(Finding)) {
	checkGitAttributes(readFile(filepath.Join(repo.root, ".gitattributes")),
		filepath.Join(repo.root, ".gitattributes"), add)
	for _, dir := range slices.Sorted(maps.Keys(hookDirs)) {
		scanHooks(filepath.Join(repo.root, dir), add)
	}
	checkGitmodules(readFile(filepath.Join(repo.root, ".gitmodules")), ".gitmodules", add)
}

// hookTriggers maps every git hook git can run to the operation that triggers
// it. A file in a hooks directory only runs when its name is in this set.
var hookTriggers = map[string]string{
	"applypatch-msg":        "runs on git am",
	"pre-applypatch":        "runs on git am",
	"post-applypatch":       "runs on git am",
	"pre-commit":            "runs on git commit",
	"pre-merge-commit":      "runs on git merge",
	"prepare-commit-msg":    "runs on git commit",
	"commit-msg":            "runs on git commit",
	"post-commit":           "runs on git commit",
	"pre-rebase":            "runs on git rebase",
	"post-checkout":         "runs on git checkout and branch switch",
	"post-merge":            "runs on git merge and pull",
	"pre-push":              "runs on git push",
	"pre-receive":           "runs on the receiving side of git push",
	"update":                "runs on the receiving side of git push",
	"proc-receive":          "runs on the receiving side of git push",
	"post-receive":          "runs on the receiving side of git push",
	"post-update":           "runs on the receiving side of git push",
	"push-to-checkout":      "runs on the receiving side of git push",
	"pre-auto-gc":           "runs on automatic git gc",
	"post-rewrite":          "runs on git commit --amend and rebase",
	"sendemail-validate":    "runs on git send-email",
	"fsmonitor-watchman":    "runs on every git command when fsmonitor is set",
	"p4-changelist":         "runs on git p4",
	"p4-prepare-changelist": "runs on git p4",
	"p4-post-changelist":    "runs on git p4",
	"p4-pre-submit":         "runs on git p4",
	"post-index-change":     "runs on every index write",
	"reference-transaction": "runs on every reference update",
}

// hookDirs are the directories a repo can use for a tracked hooks path, such as
// a core.hooksPath directory that is under version control.
var hookDirs = map[string]bool{
	".githooks": true, ".husky": true, "hooks": true, ".git-hooks": true,
}

func scanHooks(dir string, add func(Finding)) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		trigger, known := hookTriggers[strings.ToLower(name)]
		if !known {
			continue
		}
		full := filepath.Join(dir, name)
		info, err := os.Lstat(full)
		if err != nil || info.IsDir() {
			continue
		}
		add(Finding{
			Severity: SeverityCritical,
			Path:     full,
			Title:    "Active git hook: " + name,
			Detail:   hookDetail(full, info, trigger),
		})
	}
}

// hookDetail explains why the hook runs. Git runs a hook that is a symlink or
// that has no execute bit, so the mode changes the wording, not the finding.
func hookDetail(path string, info os.FileInfo, trigger string) string {
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			return "Active hook; " + trigger
		}
		return "Active hook via symlink to " + target + "; " + trigger
	}
	if info.Mode().Perm()&0o111 == 0 {
		return "Hook is not executable now, but runs on Windows and after chmod; " + trigger
	}
	return "Active hook; " + trigger
}

// The checks here cover the git files that bind a path to an external command:
// .gitattributes, .git/info/attributes, and .gitmodules. The bound command lives
// in git config or in the submodule entry, and git runs it on checkout, add,
// diff, merge, or submodule update.

// scanGitAttributes flags attribute lines that bind a path to an external
// filter, diff, or merge driver. The driver command lives in git config and is
// executed by git on checkout, add, diff, or merge.
func scanGitAttributes(repo *repoLayout, add func(Finding)) {
	checkGitAttributes(readFile(filepath.Join(repo.root, ".gitattributes")),
		filepath.Join(repo.root, ".gitattributes"), add)
	checkGitAttributes(readFile(filepath.Join(repo.gitDir, "info", "attributes")),
		filepath.Join(repo.gitDir, "info", "attributes"), add)
}

func checkGitAttributes(data []byte, path string, add func(Finding)) {
	if data == nil {
		return
	}
	for i, line := range strings.Split(string(data), "\n") {
		checkAttributeLine(line, path, i+1, add)
	}
}

// checkAttributeLine reports the external drivers that one .gitattributes line
// binds. It skips the leading path pattern, resets, and empty values.
func checkAttributeLine(line, path string, lineNo int, add func(Finding)) {
	fields := strings.Fields(stripComment(line))
	if len(fields) < 2 {
		return
	}
	for _, field := range fields[1:] {
		name, value, ok := strings.Cut(field, "=")
		if !ok || value == "" || value == "false" {
			continue
		}
		switch name {
		case "filter":
			add(Finding{SeverityHigh, path, lineNo,
				"Attribute binds filter=" + value,
				"Git runs the filter." + value + " clean/smudge command on checkout and add"})
		case "diff":
			add(Finding{SeverityHigh, path, lineNo,
				"Attribute binds diff=" + value,
				"Git runs the diff." + value + ".command or textconv command"})
		case "merge":
			if builtinMergeDrivers[value] {
				continue
			}
			add(Finding{SeverityHigh, path, lineNo,
				"Attribute binds merge=" + value,
				"Git runs the merge." + value + ".driver command"})
		}
	}
}

var builtinMergeDrivers = map[string]bool{
	"text": true, "binary": true, "union": true, "ours": true,
}

// checkGitmodules flags submodule entries that run a command when git updates
// the submodule. The file uses git config syntax.
func checkGitmodules(data []byte, path string, add func(Finding)) {
	entries, err := parseGitConfigBytes(data, path)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !strings.EqualFold(e.Section, "submodule") {
			continue
		}
		value := strings.TrimSpace(e.Value)
		switch strings.ToLower(e.Key) {
		case "update":
			if strings.HasPrefix(value, "!") {
				add(commandFinding(e, SeverityCritical, "submodule."+e.Subsection+".update",
					"Git runs this command when the submodule updates"))
			}
		case "url":
			if strings.HasPrefix(value, "ext::") {
				add(commandFinding(e, SeverityCritical, "submodule url",
					"Git runs this ext:: transport command on submodule update"))
			} else if strings.HasPrefix(value, "-") {
				add(commandFinding(e, SeverityHigh, "submodule url",
					"Submodule url starts with a dash and can inject git options"))
			}
		}
	}
}
