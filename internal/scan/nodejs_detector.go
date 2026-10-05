package scan

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// scanNodeJS covers the files a package manager or a hook framework runs while
// installing or committing: install-time scripts in package.json, npm and yarn
// config that loads a script, the pre-commit framework, and husky.
func scanNodeJS(repo *repoLayout, add func(Finding)) error {
	root := repo.root
	at := func(rel string) string { return filepath.Join(root, rel) }

	checkPackageScripts(readFile(at("package.json")), at("package.json"), add)
	checkNpmrc(readFile(at(".npmrc")), at(".npmrc"), add)
	checkYarnrc(readFile(at(".yarnrc.yml")), at(".yarnrc.yml"), add)
	checkPreCommit(readFile(at(".pre-commit-config.yaml")), at(".pre-commit-config.yaml"), add)
	checkHusky(dirExists(at(".husky")), at(".husky"), add)
	return nil
}

func checkPackageScripts(data []byte, path string, add func(Finding)) {
	scripts := packageInstallScripts(data)
	if len(scripts) == 0 {
		return
	}
	add(Finding{SeverityMedium, path, 0,
		"npm install script: " + strings.Join(scripts, ", "),
		"npm and yarn run these scripts during install"})
}

// packageInstallScripts returns the install-time scripts declared in
// package.json. It reads the raw JSON so nested "scripts" keys cannot hide.
func packageInstallScripts(data []byte) []string {
	if data == nil {
		return nil
	}
	var doc struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(stripJSONComments(data), &doc); err != nil {
		if hasToken(data, "postinstall") {
			return []string{"postinstall (unparsed)"}
		}
		return nil
	}
	wanted := []string{"preinstall", "install", "postinstall", "prepare", "prepublish", "prepublishOnly"}
	var found []string
	for _, name := range wanted {
		if _, ok := doc.Scripts[name]; ok {
			found = append(found, name)
		}
	}
	return found
}

func checkNpmrc(data []byte, path string, add func(Finding)) {
	if !hasToken(data, "onload-script") && !hasToken(data, "ignore-scripts=false") {
		return
	}
	add(Finding{SeverityHigh, path, 0,
		"npm config loads or runs a script",
		"onload-script runs arbitrary code and ignore-scripts=false enables install scripts"})
}

func checkYarnrc(data []byte, path string, add func(Finding)) {
	if !hasToken(data, "plugins") {
		return
	}
	add(Finding{SeverityMedium, path, 0,
		"Yarn plugins configured",
		"Yarn plugins are JavaScript that runs on yarn commands"})
}

func checkPreCommit(data []byte, path string, add func(Finding)) {
	if data == nil {
		return
	}
	add(Finding{SeverityMedium, path, 0,
		"pre-commit config present",
		"The pre-commit framework runs configured hooks on commit"})
}

func checkHusky(present bool, path string, add func(Finding)) {
	if !present {
		return
	}
	add(Finding{SeverityMedium, path, 0,
		"Husky hooks directory present",
		"Husky installs git hooks; check core.hooksPath and the hook files"})
}
