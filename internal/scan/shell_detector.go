package scan

import (
	"path/filepath"
	"strings"
)

// scanShell covers the shell files that run when someone changes into or builds
// the directory: the direnv file, the Makefile targets, and the setup scripts.
func scanShell(repo *repoLayout, add func(Finding)) error {
	root := repo.root
	at := func(rel string) string { return filepath.Join(root, rel) }

	checkEnvrc(readFile(at(".envrc")), at(".envrc"), add)
	checkShellSetup(readFile(at("Makefile")), at("Makefile"), add)
	walkTree(root, func(rel string) {
		if isSetupScript(rel) {
			checkShellSetup(readFile(filepath.Join(root, rel)), filepath.Join(root, rel), add)
		}
	})
	return nil
}

// isSetupScript reports shell, PowerShell, and batch files at the root or under
// scripts/. Those run during setup, build, or CI.
func isSetupScript(rel string) bool {
	dir, base := filepath.Split(rel)
	dir = filepath.ToSlash(dir)
	dir = strings.TrimSuffix(dir, "/")
	if dir != "" && dir != "scripts" && !strings.HasPrefix(dir, "scripts/") {
		return false
	}
	switch strings.ToLower(filepath.Ext(base)) {
	case ".sh", ".bash", ".zsh", ".ps1", ".bat", ".cmd":
		return true
	}
	return false
}

// shellFetchTokens reach the network and then run what they fetched. The pattern
// is what turns a download into code execution.
var shellFetchTokens = []string{
	"curl ", "wget ", "invoke-webrequest", "iwr ", "irm ", "downloadstring",
	"downloadfile", "certutil", "bitsadmin", "webclient",
}

// shellPipeTokens pipe a download straight into an interpreter.
var shellPipeTokens = []string{
	"| sh", "| bash", "|sh", "|bash", "| zsh", "|zsh", "| python", "|python",
	"iex(", "invoke-expression",
}

func checkShellSetup(data []byte, file string, add func(Finding)) {
	if len(data) == 0 {
		return
	}
	text := strings.ToLower(string(data))
	if hits := findTokens([]byte(text), shellPipeTokens...); len(hits) > 0 {
		add(Finding{SeverityCritical, file, 0,
			"Setup script pipes into an interpreter: " + strings.Join(hits, ", "),
			"The script runs what it downloads, so the payload can change after review"})
		return
	}
	if hits := findTokens([]byte(text), shellFetchTokens...); len(hits) > 0 {
		add(Finding{SeverityHigh, file, 0,
			"Setup script downloads a file: " + strings.Join(hits, ", "),
			"The script fetches a file over the network during setup or build"})
	}
}

func checkEnvrc(data []byte, path string, add func(Finding)) {
	if data == nil {
		return
	}
	add(Finding{SeverityHigh, path, 0,
		"direnv file present",
		"direnv runs .envrc on cd when the directory is allowed"})
}
