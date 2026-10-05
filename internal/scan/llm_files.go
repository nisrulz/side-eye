package scan

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// Caps keep the prompt small enough for a local model and cheap enough for a
// hosted one.
const (
	maxLLMFiles     = 40
	maxLLMFileBytes = 64 << 10
	maxLLMBytes     = 256 << 10
)

// llmSurfaceFiles always carry execution risk. The pass reads them when they
// exist, even if the listing does not mention them.
var llmSurfaceFiles = []string{
	".devcontainer.json",
	".devcontainer/devcontainer.json",
	".envrc",
	".git/config",
	".git/info/attributes",
	".gitattributes",
	".gitmodules",
	".npmrc",
	".pre-commit-config.yaml",
	".vscode/settings.json",
	".vscode/tasks.json",
	".yarnrc.yml",
	"package.json",
}

var llmSurfaceSet = func() map[string]bool {
	set := make(map[string]bool, len(llmSurfaceFiles))
	for _, p := range llmSurfaceFiles {
		set[p] = true
	}
	return set
}()

// llmSource gives the LLM pass read access to repository text and to the full
// path listing. Both remoteSource and the local adapter satisfy it.
type llmSource interface {
	file(path string) []byte
	list() []string
}

// scanLLMSource gathers the surface, calls the model, and adds its findings. It
// never fails the scan: it returns a note for the report and keeps the
// deterministic results. stderr only carries the spinner, so the note lands
// after the findings instead of before the heading.
func scanLLMSource(cfg llmConfig, src llmSource, hooksDir string, stderr io.Writer, add func(Finding)) string {
	files, listing := gatherLLMInput(src, hooksDir)
	if len(files) == 0 && len(listing) == 0 {
		return dim("→ LLM review skipped: nothing to read in this target")
	}
	spin := startSpinner(stderr, "LLM scan processing...")
	err := scanLLM(cfg, listing, files, add)
	spin.Stop()
	if err != nil {
		return llmNote(cfg, err)
	}
	return cyan("→ LLM review read " + plural(len(files), "file"))
}

// llmNote turns an LLM failure into one line. A timeout gets its own wording,
// because the fix is a longer wait rather than a different endpoint.
func llmNote(cfg llmConfig, err error) string {
	if isTimeout(err) {
		return yellow(fmt.Sprintf("→ LLM review timed out after %s. The checks above ran without it.",
			shortDuration(cfg.wait()))) +
			"\n" + dim(fmt.Sprintf("→ Give it more time with %s=10m, or scan without -llm.", llmEnvTimeout))
	}
	return yellow(fmt.Sprintf("→ LLM review skipped: %v. The checks above ran without it.", err))
}

// isTimeout reports if the request ran out of time, so the note can name the
// timeout instead of dumping the transport error at the reader.
func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || os.IsTimeout(err) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// shortDuration renders a wait the way a person would say it: 90s, 1m30s, 5m.
func shortDuration(d time.Duration) string {
	if d >= time.Minute && d%time.Minute == 0 {
		return d.Round(time.Minute).String()
	}
	return d.Round(time.Second).String()
}

// gatherLLMInput returns the bounded file set and the listing for the model.
func gatherLLMInput(src llmSource, hooksDir string) ([]llmFile, []string) {
	listing := capList(src.list())
	var files []llmFile
	total := 0
	for _, p := range llmCandidates(listing, hooksDir) {
		if len(files) >= maxLLMFiles || total >= maxLLMBytes {
			break
		}
		data := src.file(p)
		if data == nil || !isTextData(data) {
			continue
		}
		content := string(capBytes(data, maxLLMFileBytes))
		files = append(files, llmFile{Path: p, Content: content})
		total += len(content)
	}
	return files, listing
}

// llmCandidates returns the unique paths to read: the known surface files
// first, then the listing entries that match the execution-surface rules.
func llmCandidates(listing []string, hooksDir string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	for _, p := range llmSurfaceFiles {
		add(p)
	}
	for _, p := range listing {
		if llmSelected(p, hooksDir) {
			add(p)
		}
	}
	return out
}

// llmSelected reports if a path belongs to the execution surface.
func llmSelected(p, hooksDir string) bool {
	if llmSurfaceSet[p] {
		return true
	}
	base := path.Base(p)
	dir := path.Dir(p)
	switch {
	case hooksDir != "" && strings.HasPrefix(p, hooksDir+"/"):
		return true
	case dir == ".git/hooks" || hookDirs[dir]:
		return true
	case isWorkflowFile(p, base):
		return true
	case base == "Makefile" || base == "makefile" || strings.HasPrefix(base, "Dockerfile"):
		return true
	case isRootScript(p, base):
		return true
	}
	return false
}

func isWorkflowFile(p, base string) bool {
	if strings.HasPrefix(p, ".github/workflows/") {
		return strings.HasSuffix(base, ".yml") || strings.HasSuffix(base, ".yaml")
	}
	return p == ".gitlab-ci.yml" || p == ".circleci/config.yml"
}

// isRootScript reports shell, PowerShell, and batch files at the root or under
// scripts/. Those run during setup, build, or CI.
func isRootScript(p, base string) bool {
	dir := path.Dir(p)
	if dir != "." && dir != "scripts" && !strings.HasPrefix(dir, "scripts/") {
		return false
	}
	switch strings.ToLower(path.Ext(base)) {
	case ".sh", ".bash", ".zsh", ".ps1", ".bat", ".cmd":
		return true
	}
	return false
}

func capList(names []string) []string {
	if len(names) <= maxListEntries {
		return names
	}
	return names[:maxListEntries]
}

// capBytes truncates a file and marks the cut so the model knows the content is
// incomplete.
func capBytes(data []byte, n int) []byte {
	if len(data) <= n {
		return data
	}
	out := make([]byte, 0, n+32)
	out = append(out, data[:n]...)
	return append(out, []byte("\n... (truncated by side-eye)")...)
}

// isTextData rejects binary content so the prompt stays readable.
func isTextData(data []byte) bool {
	limit := len(data)
	if limit > 8000 {
		limit = 8000
	}
	for i := 0; i < limit; i++ {
		if data[i] == 0 {
			return false
		}
	}
	return true
}

// localSource adapts a local repository to llmSource. Worktree paths read from
// root; `.git/...` paths read from gitDir, which also covers bare repos.
type localSource struct {
	root   string
	gitDir string
	names  []string
}

func newLocalSource(repo *repoLayout) *localSource {
	names := walkNames(repo.root)
	// A bare repo has no worktree of its own, so prefix the listing and let
	// file() map that prefix back onto the git directory.
	if repo.bare {
		for i, name := range names {
			names[i] = ".git/" + name
		}
	}
	return &localSource{root: repo.root, gitDir: repo.gitDir, names: names}
}

func (s *localSource) list() []string { return s.names }

func (s *localSource) file(p string) []byte {
	if strings.HasPrefix(p, ".git/") {
		if s.gitDir == "" {
			return nil
		}
		return readFile(filepath.Join(s.gitDir, strings.TrimPrefix(p, ".git/")))
	}
	return readFile(filepath.Join(s.root, p))
}

// localHooksDir returns the repo-relative hooks directory so the pass reads
// files under a custom core.hooksPath.
func localHooksDir(repo *repoLayout) string {
	if repo.gitDir == "" {
		return ""
	}
	entries, err := parseGitConfigTree(filepath.Join(repo.gitDir, "config"))
	if err != nil {
		return ""
	}
	rel, err := filepath.Rel(repo.root, repo.hooksDir(entries))
	if err != nil || strings.HasPrefix(rel, "..") {
		return ""
	}
	rel = filepath.ToSlash(rel)
	if repo.bare {
		rel = path.Join(".git", rel)
	}
	return rel
}
