package scan

import (
	"path/filepath"
	"strings"
)

// scanVSCode covers the files VS Code runs when it opens a folder: a task with
// runOn folderOpen, a setting that allows tasks to run, and the dev container
// lifecycle commands.
func scanVSCode(repo *repoLayout, add func(Finding)) error {
	root := repo.root
	at := func(rel string) string { return filepath.Join(root, rel) }

	checkVSCodeTasks(readFile(at(".vscode/tasks.json")), at(".vscode/tasks.json"), add)
	checkVSCodeSettings(readFile(at(".vscode/settings.json")), at(".vscode/settings.json"), add)
	checkDevcontainer(readFile(at(".devcontainer/devcontainer.json")), at(".devcontainer/devcontainer.json"), add)
	checkDevcontainer(readFile(at(".devcontainer.json")), at(".devcontainer.json"), add)
	return nil
}

func checkVSCodeTasks(data []byte, path string, add func(Finding)) {
	if data == nil {
		return
	}
	if hasToken(data, "folderOpen") {
		add(Finding{SeverityHigh, path, 0,
			"VS Code auto-run task",
			"A task has runOn folderOpen; it runs when the folder opens"})
	}
	addTaskCommandFindings(data, path, add)
}

// taskFetchTokens reach the network or a remote host when a task runs.
var taskFetchTokens = []string{
	"curl ", "wget ", "certutil ", "bitsadmin ", "invoke-webrequest",
	"iwr ", "irm ", "downloadstring", "downloadfile", "webclient",
}

// taskShellTokens hand control to a shell or an interpreter.
var taskShellTokens = []string{
	"cmd.exe", "cmd /c", "powershell", "pwsh", "bash -c", "sh -c", "| sh", "| bash",
}

// addTaskCommandFindings flags tasks that fetch and run code. A task does not
// need runOn folderOpen to be dangerous: the default task and the run on open
// behaviour of VS Code are enough, and every command here runs on Windows.
func addTaskCommandFindings(data []byte, path string, add func(Finding)) {
	text := strings.ToLower(string(data))
	if hits := findTokens([]byte(text), taskFetchTokens...); len(hits) > 0 {
		add(Finding{SeverityCritical, path, 0,
			"VS Code task fetches remote code: " + strings.Join(hits, ", "),
			"The task command downloads and runs code; check what it pipes into"})
		return
	}
	if hits := findTokens([]byte(text), taskShellTokens...); len(hits) > 0 {
		add(Finding{SeverityHigh, path, 0,
			"VS Code task runs a shell: " + strings.Join(hits, ", "),
			"The task command starts a shell or interpreter when the folder opens"})
	}
}

func checkVSCodeSettings(data []byte, path string, add func(Finding)) {
	for _, token := range findTokens(data, "allowAutomaticTasks", "task.autoDetect", "security.workspace.trust.enabled") {
		add(Finding{SeverityMedium, path, 0,
			"VS Code setting: " + token,
			"This setting can let tasks run or trust be bypassed on open"})
	}
}

func checkDevcontainer(data []byte, path string, add func(Finding)) {
	for _, token := range []string{"initializeCommand", "onCreateCommand", "updateContentCommand", "postCreateCommand", "postStartCommand", "postAttachCommand"} {
		if strings.Contains(string(data), token) {
			add(Finding{SeverityHigh, path, 0,
				"Dev container command: " + token,
				"The dev container runs this command when the container starts"})
		}
	}
}
