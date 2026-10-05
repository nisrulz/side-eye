# LLM scan

The LLM scan is an optional review pass.
It sends the repository execution surface to an OpenAI-compatible chat endpoint and asks the model for attack paths the built-in rules miss.
It is off by default.
Pass `-llm` to turn it on.

## Environment

| Variable | Required | Meaning |
| --- | --- | --- |
| `SIDE_EYE_LLM_URL` | yes | OpenAI-compatible base URL, for example `http://localhost:11434/v1` |
| `SIDE_EYE_LLM_TOKEN` | no | Bearer token. Empty means no `Authorization` header, so Ollama works with no token |
| `SIDE_EYE_LLM_MODEL` | yes | Model name, for example `llama3.1` or `gpt-4o-mini` |
| `SIDE_EYE_LLM_PROMPT_FILE` | no | Path to a text file with the general prompt. Empty or missing keeps the default |
| `SIDE_EYE_LLM_TIMEOUT` | no | Wait for one review request, such as `90s` or `10m`. Default `5m` |

The URL can be a base URL or the full completions URL.
When it does not end in `/chat/completions`, the tool appends that path.

`-llm` with a missing URL or model prints an error and returns exit code 2.
Without `-llm`, side-eye never reads these variables and never calls the network.

## Enable

Ollama, no token:

```bash
export SIDE_EYE_LLM_URL=http://localhost:11434/v1
export SIDE_EYE_LLM_MODEL=llama3.1
side-eye -llm .
```

Hosted endpoint with a token:

```bash
export SIDE_EYE_LLM_URL=https://api.openai.com/v1
export SIDE_EYE_LLM_TOKEN=sk-...
export SIDE_EYE_LLM_MODEL=gpt-4o-mini
side-eye -llm https://github.com/example/project
```

The pass runs for local paths, remote URLs, and ZIP files.

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

## Timeout

`SIDE_EYE_LLM_TIMEOUT` sets how long one review request waits, as a Go duration
such as `90s` or `10m`. The default is `5m`, because a local model reading the
whole execution surface takes minutes. An empty or unusable value keeps the
default, so a typo cannot remove the timeout.

A local model that is still loading its weights is the usual reason for a
timeout. Raise the value rather than dropping `-llm`:

```bash
SIDE_EYE_LLM_TIMEOUT=10m side-eye -llm .
```

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

## Privacy

The pass sends repository file content and paths to the endpoint in `SIDE_EYE_LLM_URL`.
Run it only with an endpoint you trust.
Use a local Ollama endpoint when the content must stay on the machine.
