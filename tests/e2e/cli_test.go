package e2e

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestE2ELocalClean checks a repo with no risky configuration exits 0 and
// prints the all-clear. A false positive here makes the tool untrustworthy.
func TestE2ELocalClean(t *testing.T) {
	root := makeRepo(t, "[core]\n\trepositoryformatversion = 0\n")

	res := runSideEye(t, root)
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstderr: %s\nstdout: %s", res.exit, res.stderr, res.stdout)
	}
	if !strings.Contains(res.stdout, "No code that runs on clone, open, or commit") {
		t.Errorf("clean output = %q", res.stdout)
	}

	resJSON := runSideEye(t, "-json", root)
	if got := scanJSON(t, resJSON.stdout); len(got) != 0 {
		t.Errorf("clean json findings = %v, want none", titles(got))
	}
}

// TestE2ELocalHumanOutput checks the human report names the risk and tells the
// user what to do, so a non-expert can act on it.
func TestE2ELocalHumanOutput(t *testing.T) {
	root := makeRepo(t, "[core]\n\trepositoryformatversion = 0\n")
	writeFile(t, filepath.Join(root, "package.json"), `{"scripts":{"postinstall":"node x.js"}}`)

	res := runSideEye(t, root)
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0 (default -fail-on is high)", res.exit)
	}
	for _, want := range []string{"😒", "🟠 MEDIUM", "👉", "npm install script"} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("human output missing %q\n%s", want, res.stdout)
		}
	}
}

// TestE2EFailOn checks the exit code follows the worst finding against the
// -fail-on threshold, so CI can gate on the chosen severity.
func TestE2EFailOn(t *testing.T) {
	root := makeRepo(t, "[core]\n\trepositoryformatversion = 0\n")
	writeFile(t, filepath.Join(root, "package.json"), `{"scripts":{"postinstall":"node x.js"}}`)

	cases := []struct {
		flag string
		exit int
	}{
		{"critical", 0},
		{"high", 0},
		{"medium", 1},
		{"low", 1},
		{"none", 0},
	}
	for _, c := range cases {
		res := runSideEye(t, "-fail-on", c.flag, root)
		if res.exit != c.exit {
			t.Errorf("-fail-on %s: exit = %d, want %d", c.flag, res.exit, c.exit)
		}
	}
}

// TestE2EInvalidFailOn checks a bad threshold is rejected with a usage error
// instead of scanning against the wrong bar.
func TestE2EInvalidFailOn(t *testing.T) {
	res := runSideEye(t, "-fail-on", "bogus", t.TempDir())
	if res.exit != 2 {
		t.Errorf("exit = %d, want 2\nstdout: %s", res.exit, res.stdout)
	}
	if !strings.Contains(res.stderr, "Invalid -fail-on") {
		t.Errorf("stderr = %q", res.stderr)
	}
}

// TestE2EPlainDir checks a directory with no .git is scanned instead of
// refused, and the output names the git checks it could not run.
func TestE2EPlainDir(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".vscode", "tasks.json"), `{"tasks":[{"runOn":"folderOpen"}]}`)

	res := runSideEye(t, root)
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1\nstdout: %s", res.exit, res.stdout)
	}
	if !strings.Contains(res.stdout, "Directory scan") {
		t.Errorf("stdout = %q, want the directory heading", res.stdout)
	}
	if !strings.Contains(res.stdout, "no .git") {
		t.Errorf("stdout = %q, want the skipped-checks note", res.stdout)
	}
}
