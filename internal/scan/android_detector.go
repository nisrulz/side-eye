package scan

import (
	"path"
	"strings"
)

// scanAndroid covers the build files an Android project runs. Gradle executes
// the build script itself, CMake runs commands while it configures, the NDK
// build runs shell in the makefiles, and adb pushes code to a connected device.
//
// Every check here reads a file, never runs a build.
func scanAndroid(repo *repoLayout, add func(Finding)) error {
	scanAndroidSource(newLocalWorktree(repo), add)
	return nil
}

// scanAndroidSource runs the Android checks against any worktree, so a ZIP and
// a URL scan report the same Gradle, CMake, NDK, adb, and keystore findings a
// local scan does. The whole Android surface used to be local-only, so those
// scans reported a project carrying a hostile build script as clean.
func scanAndroidSource(src worktreeSource, add func(Finding)) {
	checkGradleWrapper(src.file("gradle/wrapper/gradle-wrapper.properties"),
		src.reportPath("gradle/wrapper/gradle-wrapper.properties"), add)
	checkGradleProperties(src.file("gradle.properties"), src.reportPath("gradle.properties"), add)
	checkSigningConfig(src.file("app/build.gradle"), src.reportPath("app/build.gradle"), add)
	checkLocalProperties(src.file("local.properties"), src.reportPath("local.properties"), add)
	checkADB(src.file(".vscode/tasks.json"), src.reportPath(".vscode/tasks.json"), add)
	checkADB(src.file("Makefile"), src.reportPath("Makefile"), add)

	for _, rel := range src.list() {
		file := src.reportPath(rel)
		switch base := path.Base(rel); {
		case isGradleScript(base):
			checkGradleScript(src.file(rel), file, add)
		case base == "CMakeLists.txt":
			checkCMake(src.file(rel), file, add)
		case base == "Android.mk" || base == "Android.bp":
			checkNDKMakefile(src.file(rel), file, add)
		case isKeystore(rel):
			add(Finding{SeverityHigh, file, 0,
				"Signing key in the tree",
				"A keystore lets anyone sign as the app author; keep it out of the project"})
		}
	}
}

func isGradleScript(base string) bool {
	if base == "build.gradle" || base == "settings.gradle" {
		return true
	}
	return strings.HasSuffix(base, ".gradle") || strings.HasSuffix(base, ".gradle.kts")
}

// gradleExecTokens are the calls a Gradle script makes that run a command.
// Gradle runs the script before any task, so these execute on a plain
// ./gradlew tasks.
var gradleExecTokens = []string{
	"runtime.getruntime", "processbuilder", "exec {", "exec(", "commandline",
	"project.exec", "providers.exec",
}

// gradleDownloadTokens fetch a file over the network while the script runs.
// A plain http:// is not here on purpose: every Gradle build names repository
// URLs, and a buildConfigField often carries a plain or internal URL.
var gradleDownloadTokens = []string{
	"curl ", "wget ", "new url(", "urlopen", "openstream",
}

// gradleLoadTokens pull a script or a classpath in at configuration time.
// They match on identifier boundaries, so `classpath(` matches but
// prodReleaseRuntimeClasspath does not.
//
// `apply false` is not here. It is how every Kotlin DSL project pins a plugin
// version without applying it, so it means nothing on its own.
var gradleLoadTokens = []string{
	"apply from:", "apply(from", "buildscript {", "classpath(", "initscript",
}

func checkGradleScript(data []byte, file string, add func(Finding)) {
	if len(data) == 0 {
		return
	}
	// Comments come out first: a Gradle script documents itself about
	// classpaths, and the documentation runs no code.
	text := strings.ToLower(string(stripGradleComments(data)))
	if hits := findWordTokens(text, gradleDownloadTokens...); len(hits) > 0 {
		add(Finding{SeverityCritical, file, 0,
			"Gradle script downloads a file: " + strings.Join(hits, ", "),
			"Gradle runs this script before any task, so the download happens on the first build"})
		return
	}
	if hits := findWordTokens(text, gradleExecTokens...); len(hits) > 0 {
		add(Finding{SeverityCritical, file, 0,
			"Gradle script runs a command: " + strings.Join(hits, ", "),
			"Gradle executes this while it configures the project, before any task"})
		return
	}
	if hits := findWordTokens(text, gradleLoadTokens...); len(hits) > 0 {
		add(Finding{SeverityLow, file, 0,
			"Gradle script loads another script: " + strings.Join(hits, ", "),
			"apply from and buildscript run code from another file; every multi-module build uses them"})
	}
}

// checkGradleWrapper reads the distribution the wrapper downloads and runs. A
// URL on another host means the build runs a distribution the project chose.
func checkGradleWrapper(data []byte, file string, add func(Finding)) {
	if len(data) == 0 {
		return
	}
	url := configValue(string(data), "distributionUrl")
	if url == "" || strings.HasPrefix(url, "https://services.gradle.org/") {
		return
	}
	add(Finding{SeverityHigh, file, 0,
		"Gradle wrapper downloads from another host",
		"distributionUrl is " + url + "; the build runs a distribution from a host the project picked"})
}

// gradleSecretKeys are the property names that hold a credential. A Gradle
// properties file is committed and shared with everyone who builds.
var gradleSecretKeys = []string{
	"storepassword", "keypassword", "ghp_", "github_token", "api_key", "apikey", "secret",
}

func checkGradleProperties(data []byte, file string, add func(Finding)) {
	if len(data) == 0 {
		return
	}
	lower := strings.ToLower(string(data))
	for _, key := range gradleSecretKeys {
		if !strings.Contains(lower, key) {
			continue
		}
		add(Finding{SeverityHigh, file, 0,
			"Gradle property looks like a secret: " + key,
			"gradle.properties is committed and shared with everyone who builds the project"})
	}
}

// checkSigningConfig flags an inline keystore password in a build script.
func checkSigningConfig(data []byte, file string, add func(Finding)) {
	for _, token := range findTokens(data, "storepassword", "keypassword", "storefile") {
		add(Finding{SeverityHigh, file, 0,
			"Signing config in build script: " + token,
			"The keystore password sits in the build script, so anyone with the project can sign as the author"})
	}
}

// checkLocalProperties flags the machine-specific SDK path file. It is normally
// gitignored, so finding it means the tree carries an absolute path.
func checkLocalProperties(data []byte, file string, add func(Finding)) {
	if data == nil {
		return
	}
	add(Finding{SeverityLow, file, 0,
		"local.properties present",
		"It holds the local SDK path and should stay out of the project"})
}

func isKeystore(rel string) bool {
	switch strings.ToLower(path.Ext(rel)) {
	case ".jks", ".keystore", ".p12", ".pfx":
		return true
	}
	return false
}

// cmakeExecTokens run a command while CMake configures, which happens on the
// first Gradle sync and on every clean build.
var cmakeExecTokens = []string{
	"execute_process", "add_custom_command", "add_custom_target", "file(download",
}

func checkCMake(data []byte, file string, add func(Finding)) {
	if hits := findTokens(data, cmakeExecTokens...); len(hits) > 0 {
		add(Finding{SeverityHigh, file, 0,
			"CMake runs a command: " + strings.Join(hits, ", "),
			"Gradle configures CMake before it compiles, so this runs on the first build"})
	}
}

// ndkShellTokens run a shell command from an ndk-build makefile or a Soong
// module.
var ndkShellTokens = []string{
	"$(shell ", "$(shell\t", "genrule", "local_ldflags", "local_ldlibs", "cmd:",
}

func checkNDKMakefile(data []byte, file string, add func(Finding)) {
	if hits := findTokens(data, ndkShellTokens...); len(hits) > 0 {
		add(Finding{SeverityHigh, file, 0,
			"NDK build file runs a command: " + strings.Join(hits, ", "),
			"ndk-build and Soong run this while they build the native code"})
	}
}

// adbTokens push or run code on a connected device or emulator.
var adbTokens = []string{
	"adb install", "adb push", "adb shell", "adb root", "adb remount",
	"adb reverse", "adb forward", "adb exec-out",
}

func checkADB(data []byte, file string, add func(Finding)) {
	if len(data) == 0 {
		return
	}
	if hits := findTokens(data, adbTokens...); len(hits) > 0 {
		add(Finding{SeverityMedium, file, 0,
			"adb runs on a device: " + strings.Join(hits, ", "),
			"These commands install or run code on whatever device is connected when the task runs"})
	}
}

// configValue returns the value of a key=value line in a properties file. A
// Gradle properties file escapes the colon in a URL, so the escapes are removed
// before the value is compared.
func configValue(text, key string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "#"))
		name, value, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(name) == key {
			return strings.NewReplacer(`\:`, ":", `\=`, "=", `\\`, `\`).Replace(strings.TrimSpace(value))
		}
	}
	return ""
}
