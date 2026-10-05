# Development

## Requirements

- Go. See `go.mod` for the version.

No third-party modules. The standard library only.

## Make commands

| Command | Description |
| --- | --- |
| `make help` | List all commands |
| `make install` | Build the latest code and install the CLI to `~/go/bin` |
| `make test` | Run all tests (unit and end-to-end) |
| `make check` | Format, vet, and test |
| `make scan` | Scan a path, repo URL, or ZIP (`ARGS="..."`) |

`make check` runs `go fmt ./...` first, so it writes formatting.
Run it before you commit.

## Layout

- `cmd/side-eye/` holds the entry point.
- `internal/scan/` holds the scan logic, the embedded prompt (`prompt.txt`), and the unit tests.
- `tests/e2e/` holds the end-to-end suite.

## Build and install

`make install` runs `go install` and puts `side-eye` in `~/go/bin`.
Use it to run the tool from any directory.

## Scan

```
# For a directory
make scan ARGS="~/example/project"

# For a GitHub repo
make scan ARGS="example/project"

# For a full GitHub repo URL
make scan ARGS="https://github.com/example/project"

# For a ZIP file
make scan ARGS="~/Downloads/project-main.zip"
```

## Release

```bash
make release VERSION=1.0.0
```

That runs `check` first, then `scripts/release.sh <X.Y.Z>`, which tags the current commit and pushes the tag.
The tag push triggers the [GitHub Actions release workflow](../.github/workflows/release.yml).
The workflow runs GoReleaser with [`.goreleaser.yaml`](../.goreleaser.yaml) to build amd64 and arm64 binaries for macOS, Linux, and Windows, publish a GitHub Release with `checksums.txt`, and attach [`scripts/install.sh`](../scripts/install.sh).
It then records build provenance for the archives and the checksum file.
The workflow pins every action and the GoReleaser version.
The installer aborts when the matching archive checksum is missing or invalid.

### What the installer verifies

The checksum alone proves the download arrived intact, not that it came from this
project's release: `checksums.txt` is fetched from the same release over the same
channel, so anyone who can replace the archive can replace the checksum beside it.
The installer therefore also verifies the SLSA build provenance the workflow
publishes, with `gh attestation verify`, and aborts when it does not match.

`gh` is not assumed to be installed. A missing CLI prints a warning naming the
command to run by hand rather than passing silently, and
`SIDE_EYE_INSTALL_SKIP_ATTESTATION=1` skips the check for an air-gapped mirror.
The installer also requires exactly one file named `side-eye` in the archive,
instead of taking the first match, so a second copy cannot be installed by
filesystem order.

## Tests

Unit tests live in `internal/scan/`: `scan_test.go`, `remote_test.go`, `zip_test.go`, `llm_test.go`, `llm_files_test.go`, and `spinner_test.go`.
They are in `package scan` so they can call unexported functions.
They cover config parsing, local findings, worktree findings, URL parsing, the remote source through `fakeSource`, ZIP archives, and the LLM config, prompt, and response parsing.

The end-to-end suite lives in `tests/e2e/` as its own package.
`TestMain` builds the `side-eye` binary once, then each test writes a temp git repo or ZIP and runs the binary against it as a black box.
Files split by concern:

| File | Covers |
| --- | --- |
| `helpers_test.go` | Binary build, process runner, JSON parsing, fixtures |
| `local_test.go` | One entry per git config rule and one worktree file per check |
| `cli_test.go` | Clean scans, human and JSON output, `-fail-on`, errors |
| `zip_test.go` | Archives with and without `.git`, evasion via include |
| `vectors_test.go` | Worktree `.git` file, bare repo, hook variants, config negatives, include chains, case-insensitive keys |
| `llm_test.go` | The `-llm` flag against a fake OpenAI-compatible endpoint |

`TestE2ELocalAllFindings` holds one entry per git config rule and one worktree file per check, so it fails when a rule stops firing.

```
go test ./...
```

## Conventions

- Terminal output uses signal emoji: `✓` success, `✗` error, `→` progress.
- Errors include the text and the fix. Do not print a raw error.
- Comments explain why, not what.
- Never print the OS username or an absolute home path. Show home paths as `~/...`.
- Findings match by title in the tests. Keep titles stable.
