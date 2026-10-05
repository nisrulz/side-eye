# Side Eye 😒

> Give a project the side-eye before you trust it.

side-eye scans for code that runs when you clone, open, install, or build. It reads files only and never executes git, a build, or a hook.

## Install

```bash
curl -sfL https://github.com/nisrulz/side-eye/releases/latest/download/install.sh | sh
```

The installer verifies the release archive against a SHA-256 checksum before it installs `side-eye` to `~/go/bin`, then adds that directory to your PATH if it is not there already. No Go needed.

To build from source, see the [development docs](docs/development.md).

## Usage

With no argument, side-eye scans the current directory. Change into a project and run it:

```bash
cd ~/example/project
side-eye
```

You can also pass a target:

```bash
# Scan a directory, git repository or not
side-eye ~/example/project

# Scan a repo by URL, no clone
side-eye https://github.com/example/project

# Scan a ZIP downloaded from GitHub or shared by someone else
side-eye ~/Downloads/project-main.zip
```

A directory without `.git` is scanned for the files that run code on open or install, such as `.vscode/tasks.json`, `package.json` install scripts, and tracked hook directories. The git-only checks need a repository, so the output says they were skipped.

A clean scan prints:

```bash
✅ No code that runs on clone, open, or commit in ~/example/project
```

A scan with findings prints one table. The detail and the action of a finding print under its title:

```text
┌─────────────┬───────────────────────┬────────────────────────────────────────┐
│ SEVERITY    │ LOCATION              │ FINDING                                │
├─────────────┼───────────────────────┼────────────────────────────────────────┤
│ 🚨 CRITICAL │ .git/hooks/pre-commit │ Active git hook: pre-commit            │
│             │                       │ ↳ Active hook; runs on git commit      │
│             │                       │ 👉 Do not clone, open, or run git here │
│             │                       │ until you review this.                 │
└─────────────┴───────────────────────┴────────────────────────────────────────┘

→ 1 risky item: 1 critical
```

The table is at most 80 columns wide. The output uses color when stdout is a terminal. Set `NO_COLOR=1` to turn the color off.

## What it checks

| Surface | Examples |
| --- | --- |
| Git | `core.hooksPath`, `filter.*.smudge`, `alias.*`, hooks in `.git/hooks` or a tracked hooks directory, `.gitattributes`, `.gitmodules` |
| VS Code | A `tasks.json` task that runs on open or pipes a download into a shell, workspace settings that allow tasks, dev container commands |
| Node.js | `postinstall` and the other install-time scripts, `.npmrc` `onload-script`, yarn plugins, pre-commit, husky |
| Shell | `.envrc`, a `Makefile` target, a setup script that downloads or pipes into `sh` |
| Android | A Gradle script that downloads or runs a command, a `distributionUrl` outside `services.gradle.org`, `execute_process` in CMake, `$(shell` in `Android.mk`, `adb install`, a `.jks` in the tree, a secret in `gradle.properties` |

The Android and Gradle checks ignore comments, match on identifier boundaries, and do not treat a plain repository URL or a `RuntimeClasspath` configuration as a risk. See the [local checks docs](docs/local-checks.md).

Each surface has its own detector file under `internal/scan/`. See the [local checks docs](docs/local-checks.md).

### LLM review (optional)

Pass `-llm` to also send the repository execution surface to an OpenAI-compatible endpoint for a second opinion:

```bash
export SIDE_EYE_LLM_URL=http://localhost:11434/v1
export SIDE_EYE_LLM_MODEL=llama3.1
side-eye -llm .
```

`SIDE_EYE_LLM_TOKEN` is optional, so an Ollama endpoint works with no token. Set `SIDE_EYE_LLM_PROMPT_FILE` to a text file to replace the built-in review prompt, and `SIDE_EYE_LLM_TIMEOUT` to `10m` when a local model needs longer than the default 5 minutes. See the [LLM scan docs](docs/llm-scan.md).

- Flags and dispatch: [architecture docs](docs/architecture.md)
- Severities and exit codes: [reporting docs](docs/reporting.md)
- Build, test, and make targets: [development docs](docs/development.md)

## Docs

Developer docs live in [docs/](docs/README.md).

## License

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
