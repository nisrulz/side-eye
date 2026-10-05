package scan

import (
	"io/fs"
	"path/filepath"
	"sort"
)

// maxTreeFiles caps one walk so a huge directory cannot stall a scan.
const maxTreeFiles = 20000

// maxListEntries caps a file listing. A listing is only a map of the
// repository, and past a few thousand entries it costs more than it tells.
const maxListEntries = 2000

// walkTree calls fn with every file path under root, relative to root and slash
// separated. It skips the directories in skipTreeDir and stops after
// maxTreeFiles entries.
//
// A walk error is not a finding, so it ends that branch quietly instead of
// failing the scan.
func walkTree(root string, fn func(rel string)) {
	count := 0
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil || rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if skipTreeDir(rel) {
				return fs.SkipDir
			}
			return nil
		}
		count++
		if count > maxTreeFiles {
			return fs.SkipAll
		}
		fn(rel)
		return nil
	})
}

// skipTreeDir drops the directories whose contents are binary, generated, or
// vendored. Scanning them costs time and finds nothing.
func skipTreeDir(rel string) bool {
	switch rel {
	case ".git/objects", ".git/logs", ".git/lfs", ".git/modules",
		"node_modules", "vendor", ".venv", ".tox", ".next", "dist", "build":
		return true
	}
	return false
}

// walkNames returns the sorted listing of files under root, capped at
// maxListEntries.
func walkNames(root string) []string {
	var names []string
	walkTree(root, func(rel string) {
		if len(names) >= maxListEntries {
			return
		}
		names = append(names, rel)
	})
	sort.Strings(names)
	return names
}
