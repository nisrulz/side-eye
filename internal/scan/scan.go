package scan

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// repoLayout holds the paths a scan needs. The tool never runs git, so it
// resolves these from the filesystem.
type repoLayout struct {
	root   string // working tree root
	gitDir string // .git directory (or the repo itself when bare)
	bare   bool
	plain  bool // no git directory, so only the working tree is scanned
}

func discoverRepo(path string) (*repoLayout, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", abs)
	}

	dotGit := filepath.Join(abs, ".git")
	if fi, err := os.Lstat(dotGit); err == nil {
		if fi.IsDir() {
			return &repoLayout{root: abs, gitDir: dotGit}, nil
		}
		gitDir, err := readGitFile(dotGit, abs)
		if err != nil {
			return nil, err
		}
		return &repoLayout{root: abs, gitDir: gitDir}, nil
	}

	if isGitDir(abs) {
		return &repoLayout{root: abs, gitDir: abs, bare: true}, nil
	}
	// Not a repository. The working tree still holds files that run code on
	// open or install, so scan it and report that the git checks were skipped.
	return &repoLayout{root: abs, plain: true}, nil
}

func readGitFile(path, base string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(line), "gitdir:") {
			target := strings.TrimSpace(line[len("gitdir:"):])
			if !filepath.IsAbs(target) {
				target = filepath.Join(base, target)
			}
			return filepath.Clean(target), nil
		}
	}
	return "", fmt.Errorf("%s does not point to a git directory", path)
}

func isGitDir(path string) bool {
	_, errHead := os.Stat(filepath.Join(path, "HEAD"))
	_, errConfig := os.Stat(filepath.Join(path, "config"))
	return errHead == nil && errConfig == nil
}

// hooksDir returns the directory git reads hooks from, honouring core.hooksPath.
//
// A hooksPath that resolves outside the scanned tree is ignored and the default
// is used. The value comes out of a repository-controlled .git/config, and
// scanHooks lists the directory it is given, so an absolute path or a `../`
// chain would turn a scan into a probe of the local filesystem. The ZIP path
// already refuses those, in zipHooksDir; this keeps the local path consistent
// with it.
func (r *repoLayout) hooksDir(entries []gitConfigEntry) string {
	for _, e := range entries {
		if !strings.EqualFold(e.Section, "core") || !strings.EqualFold(e.Key, "hooksPath") {
			continue
		}
		if hooks, ok := containedPath(r.root, r.root, e.Value); ok {
			return hooks
		}
		return r.defaultHooksDir()
	}
	return r.defaultHooksDir()
}

// defaultHooksDir is where git looks when core.hooksPath is unset or unusable.
func (r *repoLayout) defaultHooksDir() string {
	if r.gitDir == "" {
		return ""
	}
	return filepath.Join(r.gitDir, "hooks")
}

// scanRepo resolves the target and runs every detector over it. The detectors
// report through add, so scanRepo only collects what they found.
func scanRepo(path string) (*repoLayout, []Finding, error) {
	repo, err := discoverRepo(path)
	if err != nil {
		return nil, nil, err
	}

	var findings []Finding
	add := func(f Finding) { findings = append(findings, f) }
	for _, d := range detectors {
		if err := d(repo, add); err != nil {
			return repo, findings, err
		}
	}
	return repo, findings, nil
}
