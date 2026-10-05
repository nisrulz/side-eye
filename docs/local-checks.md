# Local checks

A local scan runs a list of detectors over one directory. Each detector owns a
surface and reads files only. Nothing here runs git, a build, or a hook.

## Dispatch

`scanRepo` in `scan.go` resolves the target with `discoverRepo`, then runs every
entry in `detectors`:

```go
var detectors = []detector{
	scanGit,
	scanVSCode,
	scanNodeJS,
	scanShell,
	scanAndroid,
}
```

A detector has the shape `func(*repoLayout, func(Finding)) error`.
It reports through the `add` callback and returns an error only when the target
cannot be read at all, because that makes the whole scan untrustworthy.

| File | Detector | Surface |
| --- | --- | --- |
| `git_detector.go` | `scanGit` | Config keys, hooks, `.gitattributes`, `.gitmodules` |
| `git_config_rules.go` | `evalConfig` | The config rule engine `scanGit` calls |
| `vscode_detector.go` | `scanVSCode` | `tasks.json`, `settings.json`, dev containers |
| `nodejs_detector.go` | `scanNodeJS` | `package.json`, `.npmrc`, `.yarnrc.yml`, pre-commit, husky |
| `shell_detector.go` | `scanShell` | `.envrc`, the `Makefile`, setup scripts |
| `android_detector.go` | `scanAndroid` | Gradle, CMake, the NDK, adb, signing keys |

Adding a surface means writing one file and adding one line to `detectors`.

## Running against a ZIP or a URL

`scanShell` and `scanAndroid` are thin wrappers. The bodies live in
`scanShellSource` and `scanAndroidSource`, which take a `worktreeSource` instead
of a `repoLayout`:

```go
type worktreeSource interface {
	file(path string) []byte
	list() []string
	reportPath(rel string) string
}
```

`scanRemoteSource` calls both, so a ZIP and a URL run the same worktree checks a
directory does. This is why the check bodies are written against the interface and
not against `readFile` and `walkTree`: a detector that reads the local filesystem
directly drops out of every other target silently, and the only symptom is that
a scan of a hostile ZIP comes back clean.

`reportPath` is the one thing the two paths disagree on. A local scan records an
absolute path and lets `location` print it relative to the root; the other targets
record the tracked path, since there is no local path to name.

`TestScanRemoteSourceRunsTheSameSurfaceAsALocalScan` in `remote_test.go` runs both
paths over one set of files and compares finding titles, so a check added to one
surface and forgotten in the other fails a test instead of reopening the gap.

## Directory without git

`discoverRepo` accepts any directory. When it finds no `.git` and the directory
is not a bare repository, `repoLayout.plain` is set and `gitDir` stays empty.

`scanGit` then runs only the checks that work without a git directory:

- `checkGitattributes` on `.gitattributes`, because git applies it as soon as the directory becomes a repository.
- The tracked hook directories in `hookDirs`, for the same reason.
- `checkGitmodules`.

The config rule engine and the resolved hooks directory are skipped.
`printPlainAdvice` prints the two lines that say so, so a clean directory scan is
not read as a clean repository.

## Tree walk

`walkTree` in `tree.go` lists every file under a root as a slash-separated
relative path. It skips the directories in `skipTreeDir` and stops after
`maxTreeFiles`. The shell and Android detectors use it directly, and
`walkNames` builds the capped listing the LLM pass reads.

A walk error is not a finding, so it ends that branch quietly.

## Hooks

`scanHooks` in `git_detector.go` reads the hooks directory.
`repoLayout.hooksDir` returns `core.hooksPath` when the config sets it, else `.git/hooks`.

A file produces a finding only when its lowercased name is in `hookTriggers`.
Sample files do not match, so `pre-commit.sample` is ignored.

The detail text changes for two cases, because both can still run:

- A symlink reports its target.
- A file without the execute bit still runs on Windows and after `chmod`.

## Attributes

`scanGitAttributes` in `git_detector.go` reads `.gitattributes` and `.git/info/attributes`.
A field that binds `filter=`, `diff=`, or a non-builtin `merge=` produces a finding.
`builtinMergeDrivers` lists the safe merge drivers: `text`, `binary`, `union`, `ours`.

## Gitmodules

`checkGitmodules` parses `.gitmodules` with the same parser as git config.
It flags:

- `submodule.*.update` when the value starts with `!`.
- `submodule url` when the value starts with `ext::` or `-`.

## Editor and package manager files

`scanVSCode` and `scanNodeJS` probe a fixed list of paths:

| Path | Check |
| --- | --- |
| `.vscode/tasks.json` | A task with `runOn` `folderOpen`, or a command that fetches remote code or starts a shell |
| `.vscode/settings.json` | `allowAutomaticTasks`, `task.autoDetect`, `security.workspace.trust.enabled` |
| `.devcontainer/devcontainer.json`, `.devcontainer.json` | Lifecycle commands |
| `.envrc` | direnv file present |
| `package.json` | Install-time scripts |
| `.npmrc` | `onload-script`, `ignore-scripts=false` |
| `.pre-commit-config.yaml` | Config present |
| `.yarnrc.yml` | Plugins |
| `.husky/` | Hooks directory present |

`packageInstallScripts` reads the raw JSON after `stripJSONComments`, so comments and nested keys do not hide a script.
The install-time script names are `preinstall`, `install`, `postinstall`, `prepare`, `prepublish`, and `prepublishOnly`.

## VS Code task commands

`addTaskCommandFindings` reads `.vscode/tasks.json` and matches tokens case-insensitively in the whole file.
A task that fetches from the network is `CRITICAL`: the token list is `taskFetchTokens`, which covers `curl`, `wget`, `certutil`, `bitsadmin`, PowerShell download cmdlets, and the `webclient` class.
A task that only starts a shell or interpreter is `HIGH`: the token list is `taskShellTokens`.

The command check does not depend on `folderOpen`.
A task runs when VS Code trusts the folder, through the default build task or through `runOn`.
Sources: the ThreatLocker multi-stage infostealer write-up and the Undercode Testing tasks.json backdoor write-up.

## Shell setup

`scanShell` reads `.envrc` and the root `Makefile`, then walks for the setup
scripts `isSetupScript` selects: shell, PowerShell, and batch files at the root
or under `scripts/`.

A script that pipes a download into an interpreter is `CRITICAL`
(`shellPipeTokens`), because the payload can change after review.
A script that only downloads is `HIGH` (`shellFetchTokens`).
The pipe check returns first, so a piped download is reported once.

## Android builds

`scanAndroid` reads the build files an Android project runs. Gradle evaluates a
build script before any task, so a script that runs a command or fetches code is
`CRITICAL`.

| File | Check |
| --- | --- |
| `*.gradle`, `*.gradle.kts` | `gradleDownloadTokens` then `gradleExecTokens`, then `gradleLoadTokens` |
| `gradle/wrapper/gradle-wrapper.properties` | `distributionUrl` outside `services.gradle.org` |
| `gradle.properties` | `gradleSecretKeys` present in the shared file |
| `app/build.gradle` | Inline `storePassword`, `keyPassword`, or `storeFile` |
| `local.properties` | Machine-specific SDK path, normally gitignored |
| `CMakeLists.txt` | `cmakeExecTokens`, which run while Gradle configures |
| `Android.mk`, `Android.bp` | `ndkShellTokens`, which run during the native build |
| `*.jks`, `*.keystore`, `*.p12`, `*.pfx` | Signing key in the tree |
| `.vscode/tasks.json`, `Makefile` | `adbTokens`, which act on a connected device |

`configValue` unescapes `\:` and `\=` first, because a Gradle properties file
escapes the colon in a URL.

### Matching Gradle and Kotlin correctly

A Gradle build script is full of words that look dangerous and are not, so
`checkGradleScript` does three things before it matches:

1. `stripGradleComments` removes `//` and `/* */` comments, keeping quoted
   strings so a URL inside one survives. A well-written build script documents
   itself about classpaths, and documentation runs no code.
2. The text is lowercased, so matching is case-insensitive.
3. `findWordTokens` checks identifier boundaries. `classpath(` does not match
   `configuration("prodReleaseRuntimeClasspath")`.

A bare `http://` is deliberately not a token. Every Gradle project names
repository URLs, and a `buildConfigField` often carries a plain or internal URL.
Only an actual fetch counts: `curl`, `wget`, `new URL(`, `openStream`.

`apply false` is not a token either. It is how every Kotlin DSL project pins a
plugin version without applying it.

These three rules exist because scanning Google's Now in Android first reported
one critical and three high findings on it, all false positives. That project is
now clean apart from `local.properties`.

## Shared checks

`scanRemoteSource` in `remote.go` calls the same `check*` functions for a remote target.
When you add a worktree check, add the path in the remote function too so the results stay consistent.

The shell and Android surfaces do not need that step: they run over
`worktreeSource`, so `scanRemoteSource` reaches them by calling `scanShellSource`
and `scanAndroidSource`. A new check inside either surface is picked up by every
target automatically. The parity test above is what keeps that true.