package scan

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// zipSource reads files from an archive downloaded from a git host or shared by
// another party. It serves the shared worktree checks, and it also scans a
// `.git` directory when the archive carries one.
type zipSource struct {
	display string
	archive *zip.ReadCloser
	entries map[string]*zip.File
	names   []string
	hooks   []string
}

// isZipArg reports if an argument is a local zip archive.
func isZipArg(arg string) bool {
	info, err := os.Stat(arg)
	if err != nil || info.IsDir() {
		return false
	}
	return strings.EqualFold(filepath.Ext(arg), ".zip")
}

func newZipSource(archive string) (*zipSource, error) {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", archive, err)
	}

	s := &zipSource{display: archive, archive: zr, entries: map[string]*zip.File{}}
	prefix := commonTopDir(zr.File)
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := strings.TrimPrefix(f.Name, prefix)
		if name == "" {
			continue
		}
		s.entries[name] = f
		s.names = append(s.names, name)
		if _, ok := hookTriggers[strings.ToLower(path.Base(name))]; ok && hookDirs[path.Dir(name)] {
			s.hooks = append(s.hooks, name)
		}
	}
	sort.Strings(s.names)
	return s, nil
}

func (s *zipSource) file(p string) []byte {
	f, ok := s.entries[p]
	if !ok {
		return nil
	}
	data, err := readZipFile(f)
	if err != nil {
		return nil
	}
	return data
}

func (s *zipSource) hookFiles() []string { return s.hooks }

// list returns every entry name in the archive.
func (s *zipSource) list() []string { return s.names }

func (s *zipSource) Close() error { return s.archive.Close() }

func (s *zipSource) hasGitDir() bool {
	for name := range s.entries {
		if strings.HasPrefix(name, ".git/") {
			return true
		}
	}
	return false
}

// scanGitDir runs the local checks that read `.git/config` and `.git/hooks`.
// It is a no-op when the archive has no `.git` directory.
func (s *zipSource) scanGitDir(add func(Finding)) {
	entries, err := parseGitConfigBytes(s.file(".git/config"), ".git/config")
	if err != nil {
		entries = nil
	}
	entries = s.followIncludes(entries, ".git/config")
	evalConfig(entries, add)

	checkGitAttributes(s.file(".git/info/attributes"), ".git/info/attributes", add)
	s.scanHookEntries(zipHooksDir(entries), add)
}

// followIncludes walks include.path directives that stay inside the archive.
func (s *zipSource) followIncludes(entries []gitConfigEntry, base string) []gitConfigEntry {
	all := entries
	seen := map[string]bool{base: true}
	queue := includePaths(entries, base)
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		if seen[p] {
			continue
		}
		seen[p] = true
		more, err := parseGitConfigBytes(s.file(p), p)
		if err != nil {
			continue
		}
		all = append(all, more...)
		queue = append(queue, includePaths(more, p)...)
	}
	return all
}

func includePaths(entries []gitConfigEntry, base string) []string {
	dir := path.Dir(base)
	var paths []string
	for _, e := range entries {
		if !isIncludeEntry(e) {
			continue
		}
		inc := strings.TrimSpace(e.Value)
		if inc == "" || path.IsAbs(inc) || strings.HasPrefix(inc, "~") {
			continue
		}
		paths = append(paths, path.Join(dir, inc))
	}
	return paths
}

// zipHooksDir resolves the hooks directory from core.hooksPath, defaulting to
// `.git/hooks`. It returns "" when the configured path cannot live in the archive.
func zipHooksDir(entries []gitConfigEntry) string {
	for _, e := range entries {
		if strings.EqualFold(e.Section, "core") && strings.EqualFold(e.Key, "hooksPath") {
			p := strings.TrimSpace(e.Value)
			if p == "" || path.IsAbs(p) || strings.HasPrefix(p, "~") {
				return ""
			}
			return path.Clean(p)
		}
	}
	return ".git/hooks"
}

func (s *zipSource) scanHookEntries(dir string, add func(Finding)) {
	if dir == "" {
		return
	}
	for _, name := range s.names {
		if path.Dir(name) != dir {
			continue
		}
		base := path.Base(name)
		trigger, ok := hookTriggers[strings.ToLower(base)]
		if !ok {
			continue
		}
		add(Finding{SeverityCritical, name, 0,
			"Active git hook: " + base,
			"Active hook; " + trigger})
	}
}

// commonTopDir returns the single top-level directory shared by every entry,
// which is how GitHub wraps a repository ZIP. It returns "" when the entries
// have no common top-level directory.
func commonTopDir(files []*zip.File) string {
	if len(files) == 0 {
		return ""
	}
	i := strings.IndexByte(files[0].Name, '/')
	if i < 0 {
		return ""
	}
	prefix := files[0].Name[:i+1]
	for _, f := range files {
		if !strings.HasPrefix(f.Name, prefix) {
			return ""
		}
	}
	return prefix
}

func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, maxReadBytes))
}
