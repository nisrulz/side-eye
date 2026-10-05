# side-eye developer docs

side-eye scans a directory or repository for configuration that runs code on checkout, commit, or open.
It reads files only. It never runs git.

File names in these docs refer to files under `internal/scan/` unless a path is given.

## Docs by function

| Doc | Covers |
| --- | --- |
| [architecture.md](architecture.md) | Entry point, scan lifecycle, module map |
| [rules.md](rules.md) | Git config rules and severity |
| [local-checks.md](local-checks.md) | Hooks, attributes, gitmodules, worktree files |
| [remote-scans.md](remote-scans.md) | URL parsing, remote and ZIP file sources |
| [llm-scan.md](llm-scan.md) | Optional LLM review pass, prompt, caps |
| [llm-servers.md](llm-servers.md) | LLM server setup, environment variables, timeout |
| [reporting.md](reporting.md) | Findings, output formats, exit codes |
| [development.md](development.md) | Build, install, test, conventions |

## At a glance

- Language: Go. See `go.mod` for the version.
- Entry point: `Run` in `internal/scan/run.go`, called by `cmd/side-eye/main.go`.
- Layout: `cmd/side-eye/` (entry point), `internal/scan/` (logic and unit tests), `tests/e2e/` (black box).
- Dependencies: the standard library, plus `github.com/fatih/color` for terminal color. Nothing here reads a repository.
- Tests: `go test ./...`, or `make check`.
