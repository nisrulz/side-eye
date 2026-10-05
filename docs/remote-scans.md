# Remote scans

side-eye can scan a repository from a URL without a clone.

## URL detection and parsing

`isRemoteArg` in `remote.go` decides local versus remote:

- An argument that exists on disk is local.
- `://` and `git@` mark a remote URL.
- `owner/repo` and `host/owner/repo` are remote.

`parseRemoteURL` uses `splitRemoteURL` to build a `remoteTarget`:

| Field | Meaning |
| --- | --- |
| `raw` | The argument as typed |
| `clone` | The clone URL, kept as typed |
| `host`, `owner`, `repo` | URL parts |
| `ref` | Branch or tag |
| `token` | API token |

`clone` is the argument's own form, not a rewritten HTTPS URL, so an SSH target
keeps working SSH: `git@github.com:owner/repo.git` clones over SSH and
`https://github.com/owner/repo.git` over HTTPS. Only when the argument carried no
scheme does `parseRemoteURL` build an HTTPS URL from the parts. The scan itself
reads through the API or a raw endpoint either way; `clone` is what the
`git clone --no-checkout` advice prints.

A `git@host:owner/repo` argument is split at the first colon after the host, so a
GitLab subgroup survives as the owner: `git@gitlab.com:group/sub/repo.git` gives
host `gitlab.com` and owner `group/sub`.

## Source interface

```go
type remoteSource interface {
	worktreeSource
	hookFiles() []string
}
```

`worktreeSource` in `detectors.go` is the shared read interface every target shape
provides — a local directory, a ZIP archive, and a remote host alike:

```go
type worktreeSource interface {
	file(path string) []byte
	list() []string
	reportPath(rel string) string
}
```

`reportPath` is the path a finding carries. A local scan holds absolute paths and
lets the report make them relative again; the other targets print the tracked
path, since there is no local path to name.

`scanRemoteSource` runs the shared worktree checks against a source.
`list` returns the full file listing for the optional LLM pass. See [llm-scan.md](llm-scan.md).
See [local-checks.md](local-checks.md) for the file list.

The shell and Android checks run over `worktreeSource` rather than reading the
local filesystem directly, which is what puts them on this path. They used to be
wired to `repoLayout` and `walkTree`, so `scanRemoteSource` never reached them and
a ZIP or URL scan reported a tree holding a `curl … | sh` setup script, or a
Gradle script that shells out, as clean.
`TestScanRemoteSourceRunsTheSameSurfaceAsALocalScan` compares the two paths so the
gap cannot reopen silently.

## GitHub source

`newGitHubSource` in `github.go` does this:

1. Resolve the ref. When the ref is empty, read `default_branch` from the API.
2. List the tree recursively with `git/trees/<ref>?recursive=1`.
3. Read each file with the contents API when a token is set, else from `raw.githubusercontent.com`.
4. Cache every file in memory.

`hookFiles` returns tracked files whose base name is in `hookTriggers` and whose directory is in `hookDirs`.
`hookDirs` contains `.githooks`, `.husky`, `hooks`, and `.git-hooks`.
`list` returns every tracked path from the tree.

The source skips paths that contain `/.git/`.

## Raw source

`newRawSource` in `raw.go` probes raw file endpoints for hosts without a tree listing.
It tries the ref, or `HEAD`, `main`, and `master` when the ref is empty.
`rawBases` gives host-specific URL prefixes for GitLab, Bitbucket, and other hosts.

`hookFiles` probes the directories in `hookDirs` and the names in `rawHookNames`.
`list` returns the probed paths that returned content.
This source is best effort.

## Zip source

`newZipSource` in `zip.go` reads a ZIP archive downloaded from a git host, such as the archive GitHub serves from a repository page.
`isZipArg` in `zip.go` marks an existing local file with a `.zip` extension.

1. `commonTopDir` finds the single top-level directory that wraps the repository, so entry names match the other sources.
2. Each file entry is indexed by its stripped path, and that path is kept for the `.git` checks.
3. `hookFiles` returns entries whose base name is in `hookTriggers` and whose directory is in `hookDirs`.
4. `file` decompresses one entry on demand and caps it at 8 MiB.
5. `list` returns every entry name.

The source satisfies `remoteSource`, so `scanRemoteSource` runs the shared checks against it.
A ZIP from another party can also carry a `.git` directory.
`scanGitDir` reads `.git/config` in memory, follows `include.path` entries that stay inside the archive, applies the config rules, reads `.git/info/attributes`, and scans the resolved hooks directory.
It resolves the hooks directory from `core.hooksPath` and defaults to `.git/hooks`.
`hasGitDir` lets the report stay quiet when the archive has no `.git` directory.

## Limits

A remote scan cannot read `.git/config` or `.git/hooks`.
Those paths exist only after a clone.
A ZIP archive has no `.git` directory unless its creator included one.
`printCloneAdvice` and `printZipAdvice` in `report.go` print the safe clone sequence for a URL scan and for a ZIP without `.git`:

```
git clone --no-checkout <url> <dir>
side-eye <dir>
git -C <dir> checkout
```
