package scan

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"
)

// remoteTarget is a parsed git host URL.
type remoteTarget struct {
	raw   string
	clone string
	host  string
	owner string
	repo  string
	ref   string
	token string
}

// isRemoteArg reports if an argument is a git host URL, not a local path.
func isRemoteArg(arg string) bool {
	if strings.Contains(arg, "://") || strings.HasPrefix(arg, "git@") {
		return true
	}
	if _, err := os.Stat(arg); err == nil {
		return false
	}
	if strings.Contains(arg, "@") && strings.Contains(arg, ":") {
		return true
	}
	parts := strings.Split(arg, "/")
	if len(parts) == 2 && !strings.HasPrefix(arg, ".") && !strings.HasPrefix(arg, "/") {
		return true
	}
	return len(parts) >= 3 && strings.Contains(parts[0], ".") && !strings.HasPrefix(arg, ".")
}

func splitRemoteURL(raw string) (host, repoPath, clone string, err error) {
	s := strings.TrimSuffix(strings.TrimSpace(raw), "/")
	switch {
	case strings.Contains(s, "://"):
		u, uerr := url.Parse(s)
		if uerr != nil {
			return "", "", "", fmt.Errorf("invalid repository url: %w", uerr)
		}
		return u.Hostname(), strings.TrimPrefix(u.Path, "/"), s, nil
	case strings.HasPrefix(s, "git@"):
		rest := strings.TrimPrefix(s, "git@")
		i := strings.IndexByte(rest, ':')
		if i < 0 {
			return "", "", "", fmt.Errorf("invalid ssh repository url %q", raw)
		}
		return rest[:i], rest[i+1:], s, nil
	default:
		parts := strings.SplitN(s, "/", 2)
		if len(parts) != 2 {
			return "", "", "", fmt.Errorf("invalid repository url %q", raw)
		}
		if strings.Contains(parts[0], ".") {
			return parts[0], parts[1], "", nil
		}
		return "github.com", s, "", nil
	}
}

func parseRemoteURL(raw, ref, token string) (*remoteTarget, error) {
	host, repoPath, clone, err := splitRemoteURL(raw)
	if err != nil {
		return nil, err
	}
	repoPath = strings.TrimSuffix(strings.Trim(repoPath, "/"), ".git")
	if host == "" || repoPath == "" {
		return nil, fmt.Errorf("repository url %q has no owner or repo", raw)
	}
	owner := path.Dir(repoPath)
	if owner == "." {
		return nil, fmt.Errorf("repository url %q has no owner", raw)
	}
	if clone == "" {
		clone = "https://" + host + "/" + owner + "/" + path.Base(repoPath) + ".git"
	}
	return &remoteTarget{
		raw: raw, clone: clone, host: host, owner: owner,
		repo: path.Base(repoPath), ref: ref, token: token,
	}, nil
}

func (t *remoteTarget) display() string {
	s := t.host + "/" + t.owner + "/" + t.repo
	if t.ref != "" {
		s += " (ref: " + t.ref + ")"
	}
	return s
}

func httpClient() *http.Client {
	return &http.Client{Timeout: 20 * time.Second}
}

// maxReadBytes caps one read of untrusted content: an HTTP response body or an
// archive entry. The files and API replies this tool reads are small, and the
// cap stops a hostile source from streaming forever.
const maxReadBytes = 8 << 20

// remoteSource gives the checks read-only access to tracked files, to the
// names of tracked hook files, and to the full file listing in the remote repo.
// It is also a worktreeSource: a ZIP and a URL scan run the same worktree checks
// a local scan does, over the tracked path.
type remoteSource interface {
	worktreeSource
	hookFiles() []string
}

// remoteReportPath is the path a finding carries for a remote or archived file.
// There is no local path to print, so it is the tracked path itself.
func remoteReportPath(rel string) string { return rel }

func scanRemote(t *remoteTarget) (remoteSource, []Finding, error) {
	src, err := newRemoteSource(t)
	if err != nil {
		return nil, nil, err
	}

	var findings []Finding
	add := func(f Finding) { findings = append(findings, f) }
	scanRemoteSource(src, add)
	return src, findings, nil
}

// newRemoteSource picks the file source for a host. GitHub has an API; other
// hosts use raw file endpoints.
func newRemoteSource(t *remoteTarget) (remoteSource, error) {
	if t.host == "github.com" {
		return newGitHubSource(t)
	}
	return newRawSource(t)
}

func scanRemoteSource(src remoteSource, add func(Finding)) {
	checkGitAttributes(src.file(".gitattributes"), ".gitattributes", add)
	checkGitmodules(src.file(".gitmodules"), ".gitmodules", add)
	checkVSCodeTasks(src.file(".vscode/tasks.json"), ".vscode/tasks.json", add)
	checkVSCodeSettings(src.file(".vscode/settings.json"), ".vscode/settings.json", add)
	checkDevcontainer(src.file(".devcontainer/devcontainer.json"), ".devcontainer/devcontainer.json", add)
	checkDevcontainer(src.file(".devcontainer.json"), ".devcontainer.json", add)
	checkPackageScripts(src.file("package.json"), "package.json", add)
	checkNpmrc(src.file(".npmrc"), ".npmrc", add)
	checkPreCommit(src.file(".pre-commit-config.yaml"), ".pre-commit-config.yaml", add)
	checkYarnrc(src.file(".yarnrc.yml"), ".yarnrc.yml", add)

	hooks := src.hookFiles()
	hasHusky := false
	for _, hook := range hooks {
		if strings.HasPrefix(hook, ".husky/") {
			hasHusky = true
		}
	}
	checkHusky(hasHusky, ".husky", add)
	for _, hook := range hooks {
		add(Finding{SeverityHigh, hook, 0,
			"Tracked hook file: " + path.Base(hook),
			"Runs if core.hooksPath points to " + path.Dir(hook) + "; husky install sets this"})
	}

	// The shell and Android checks run here too. They used to be local-only, so
	// a ZIP or a URL scan missed a setup script that pipes a download into an
	// interpreter, and the whole Gradle, CMake, NDK, adb, and keystore surface.
	// checkEnvrc is left out of the list above: scanShellSource covers it.
	scanShellSource(src, add)
	scanAndroidSource(src, add)
}

func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}
