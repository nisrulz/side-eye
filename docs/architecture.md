# Architecture

## Entry point

`cmd/side-eye/main.go` calls `Run` in `internal/scan/run.go`.
`Run` parses the flags, resolves the target, then dispatches:

- A local path goes to `runLocal`.
- A remote URL goes to `runRemote`. See [remote-scans.md](remote-scans.md).
- A local ZIP file goes to `runZip`. See [remote-scans.md](remote-scans.md).

Flags:

| Flag | Default | Meaning |
| --- | --- | --- |
| `-json` | `false` | Print findings as JSON |
| `-ref` | `""` | Remote branch or tag to scan |
| `-token` | `""` | GitHub token for private repos |
| `-fail-on` | `high` | Lowest severity that returns exit code 1 |
| `-llm` | `false` | Run the optional LLM review pass. See [llm-scan.md](llm-scan.md) |

`resolveToken` reads the flag first, then `GITHUB_TOKEN`, then `GH_TOKEN`.

## Local scan lifecycle

`runLocal` calls `scanRepo` in `scan.go`:

1. `discoverRepo` resolves `root` and `gitDir`.
   It accepts a normal `.git` directory, a `.git` file (a worktree), and a bare repo.
   A directory with no git metadata sets `repoLayout.plain` instead of failing.
2. Every detector in `detectors` runs over the target. See [local-checks.md](local-checks.md).

The git config rules, hooks, editor files, package manager files, shell setup
scripts, and Android build files each live in their own detector file.

3. When `-llm` is set, `gatherLLMInput` collects the surface and `scanLLM` adds the model findings. See [llm-scan.md](llm-scan.md).

Every check appends a `Finding` through the `add` callback.
A detector returns an error only when the target cannot be read at all.

## Remote scan lifecycle

`runRemote` parses the URL, then `scanRemote` picks a source:

- `github.com` uses `githubSource`, which reads the GitHub API.
- Other hosts use `rawSource`, which reads raw file endpoints.

Both sources satisfy the `remoteSource` interface. `scanRemoteSource` runs the shared checks against either one.
See [remote-scans.md](remote-scans.md).

## Data model

- `Finding`: severity, path, line, title, detail. See `findings.go`.
- `Severity`: `Info` < `Low` < `Medium` < `High` < `Critical`.
- `exitCodeFor` turns `highestSeverity` into the process exit code. See [reporting.md](reporting.md).

## Module map

The CLI is a thin entry point. All logic lives in the `scan` package.

| File | Responsibility |
| --- | --- |
| `cmd/side-eye/main.go` | Entry point; calls `scan.Run` |
| `internal/scan/run.go` | Flags, dispatch, remote, ZIP, and local runners |
| `internal/scan/scan.go` | Repo discovery and local orchestration |
| `internal/scan/detectors.go` | The detector contract and the registry of surfaces |
| `internal/scan/config.go` | Git config parser and include walker |
| `internal/scan/git_detector.go` | Hooks, `.gitattributes`, `.gitmodules` |
| `internal/scan/git_config_rules.go` | Config rule handlers |
| `internal/scan/vscode_detector.go` | VS Code tasks, settings, devcontainer |
| `internal/scan/nodejs_detector.go` | Install scripts, npmrc, pre-commit, husky |
| `internal/scan/shell_detector.go` | `.envrc`, Makefile, setup scripts |
| `internal/scan/android_detector.go` | Gradle, CMake, NDK, adb |
| `internal/scan/fileutil.go` | Shared file and JSON readers |
| `internal/scan/tree.go` | Capped directory walk and skip rules |
| `internal/scan/style.go` | Color, severity markers, and width helpers |
| `internal/scan/table.go` | Box table rendering and cell wrapping |
| `internal/scan/banner.go` | Wordmark on stderr |
| `internal/scan/spinner.go` | Terminal-only progress spinner |
| `internal/scan/remote.go` | URL parsing, source interface, shared checks |
| `internal/scan/github.go` | GitHub API source |
| `internal/scan/raw.go` | Raw endpoint source |
| `internal/scan/zip.go` | ZIP archive source |
| `internal/scan/findings.go` | `Finding` and `Severity` types |
| `internal/scan/report.go` | Human and JSON output |
| `internal/scan/llm.go` | LLM config, request, and response parsing |
| `internal/scan/llm_files.go` | Execution-surface gathering for the LLM pass |
| `internal/scan/prompt.txt` | Default LLM system prompt, embedded at build time |
