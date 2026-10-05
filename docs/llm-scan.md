# LLM scan

The LLM scan is an optional review pass.
It sends the repository execution surface to an OpenAI-compatible chat endpoint and asks the model for attack paths the built-in rules miss.
It is off by default.
Pass `-llm` to turn it on.

For the environment variables, the setup for local servers, and worked
examples, see [llm-servers.md](llm-servers.md).
This page covers what the pass sends and how it behaves.

## What the model reads

The pass sends two parts.

1. The full path listing of the repository.
2. The full text of execution-surface files that exist.

The surface files are:

- `.git/config`, `.git/info/attributes`, `.gitattributes`, `.gitmodules`
- Hook files under `.git/hooks`, `.husky`, `.githooks`, `.git-hooks`, `hooks`, or a custom `core.hooksPath`
- `.envrc`, `package.json`, `.npmrc`, `.yarnrc.yml`, `.pre-commit-config.yaml`
- `.vscode/tasks.json`, `.vscode/settings.json`, `.devcontainer/devcontainer.json`, `.devcontainer.json`
- `.github/workflows/*.yml`, `.github/workflows/*.yaml`, `.gitlab-ci.yml`, `.circleci/config.yml`
- `Makefile`, `Dockerfile*`, and `*.sh`, `*.bash`, `*.zsh`, `*.ps1`, `*.bat`, `*.cmd` at the root or under `scripts/`

## Prompt

`internal/scan/prompt.txt` is embedded in the binary. It is the part that decides
whether the pass is useful, so it states three things:

- **What side-eye is.** The model is a security engineer reviewing a repository
  for side-eye, which shows a developer what would run code on their machine.
- **Nothing is executed.** side-eye never runs git, a build, a hook, or a script.
  The prompt says so explicitly, because a model told only to find attack paths
  will report the project's own build commands as if the tool had run them.
- **What not to report.** Ordinary application code, build and test steps, and
  files that merely exist are named as non-findings, with the reason that a
  report full of them trains the reader to ignore it.

It also tells the model that an empty result is the expected answer.

`vagueLLMFinding` in `llm.go` drops the titles that name a category of code
rather than a way to run code, such as "Network communication" or "Application
manifest configuration". These came from scanning a real Android project, where
they were not actionable. The prompt asks for them not to be written, and the
filter drops them if a model writes them anyway.

Caps keep the payload small:

| Cap | Value |
| --- | --- |
| Full-text files | 40 |
| One file | 64 KiB |
| Total text | 256 KiB |
| Listing entries | 2000 |

A file over a cap still appears in the listing.
Binary files are skipped.

## System prompt

See [Prompt](#prompt) above for what the default says and why.

Set `SIDE_EYE_LLM_PROMPT_FILE` to a text file to replace it.
An empty or missing file keeps the default.
The override is opt-in and read only from that path, so a file inside the scanned repository cannot change the system prompt.

The response contract at the end of the prompt is fixed.
It is always appended, so a custom general prompt still gets machine-readable JSON.

## Prompt and response

The model returns only this JSON shape:

```json
{"findings":[{"severity":"critical","path":"relative/path","line":0,"title":"short label","detail":"what runs and when"}]}
```

`line` is 0 when no line applies.
The parser accepts `critical`, `high`, `medium`, `low`, and `info`, and drops entries it cannot use.
Each finding gets the title prefix `LLM: `, so model output is easy to tell from the built-in rules.

LLM findings join the normal report and the normal exit code.
`-fail-on` treats them the same as any other finding.

## Failure behavior

The pass never breaks a scan. A network error, a timeout, or an unparseable
response prints one note after the findings and continues. The built-in findings
and the exit code stay unchanged.

A timeout gets its own wording, because the fix is a longer wait and not a
different endpoint:

```text
→ LLM review timed out after 5m. The checks above ran without it.
→ Give it more time with SIDE_EYE_LLM_TIMEOUT=10m, or scan without -llm.
```

Any other failure keeps the reason, such as `endpoint returned 500 Internal
Server Error`.

The token goes in the request header only.
side-eye never prints it.
The model output is parsed as data and never executed.
