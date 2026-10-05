package scan

import (
	"path/filepath"
	"strings"
)

// detector reads one kind of surface and reports what runs code. A detector
// returns an error only when the target cannot be read at all, because that
// makes the whole scan untrustworthy.
type detector func(repo *repoLayout, add func(Finding)) error

// detectors are the surfaces side-eye covers. They are independent: each one
// looks at its own files and skips itself when the target has none of them.
var detectors = []detector{
	scanGit,
	scanVSCode,
	scanNodeJS,
	scanShell,
	scanAndroid,
}

// worktreeSource is read access to a repository's working tree: the bytes of
// any file in it, the path of every file in it, and the path to print for a
// file. A local directory, a ZIP archive, and a remote host each satisfy it.
//
// The shell and Android checks run over this interface instead of repoLayout,
// so a ZIP and a URL scan reach them too. A detector wired only to the local
// filesystem drops out of those scans silently, and that once hid a
// `curl … | sh` setup script that the local path reported as CRITICAL: the same
// tree scanned from a ZIP came back clean.
type worktreeSource interface {
	// file returns the bytes of one repository-relative file, or nil when the
	// target has no such file.
	file(path string) []byte
	// list returns the repository-relative path of every file in the worktree.
	list() []string
	// reportPath returns the path a finding should carry for a
	// repository-relative file. Local scans print an absolute path and the
	// report makes it relative again; the other targets print the tracked path.
	reportPath(rel string) string
}

// localWorktree reads a local working tree from disk.
type localWorktree struct {
	root  string
	names []string
}

func newLocalWorktree(repo *repoLayout) *localWorktree {
	return &localWorktree{root: repo.root, names: walkNames(repo.root)}
}

func (s *localWorktree) file(p string) []byte {
	return readFile(filepath.Join(s.root, filepath.FromSlash(p)))
}

func (s *localWorktree) list() []string { return s.names }

func (s *localWorktree) reportPath(rel string) string {
	return filepath.Join(s.root, filepath.FromSlash(rel))
}

// pathBase and pathDir use slash separators because every source reports
// repository-relative slash paths, even a local one on Windows.

func pathBase(rel string) string {
	if i := strings.LastIndexByte(rel, '/'); i >= 0 {
		return rel[i+1:]
	}
	return rel
}

func pathDir(rel string) string {
	if i := strings.LastIndexByte(rel, '/'); i >= 0 {
		return rel[:i]
	}
	return "."
}

// pathExt returns the extension of a repository-relative path, including the
// dot.
func pathExt(rel string) string {
	base := pathBase(rel)
	if i := strings.LastIndexByte(base, '.'); i > 0 {
		return base[i:]
	}
	return ""
}
