# LLM servers

The `-llm` pass talks to any OpenAI-compatible chat endpoint. Start your server
first, then point the pass at Ollama:

```bash
export SIDE_EYE_LLM_URL=http://localhost:11434/v1
export SIDE_EYE_LLM_MODEL=gemma4:e2b-it-qat
side-eye -llm .
```

It scans a local path, a repository URL, or a ZIP. For what the pass sends and
how it behaves on failure, see [llm-scan.md](llm-scan.md).

## Local servers

Each of these is already OpenAI-compatible, so none of them needs a token.

| Server | `SIDE_EYE_LLM_URL` | `SIDE_EYE_LLM_MODEL` |
| --- | --- | --- |
| Ollama | `http://localhost:11434/v1` | The tag you pulled, for example `gemma4:e2b-it-qat` |
| LM Studio | `http://localhost:1234/v1` | The identifier shown in the Local Server tab |
| Unsloth Studio | `http://localhost:8888/v1` | The model loaded in the studio |

Unsloth's docs show a second port, `8000`, for a model server it connects to
rather than the one it serves. Use the port Unsloth prints at startup if it
differs.

A hosted endpoint needs a token:

```bash
export SIDE_EYE_LLM_URL=https://api.openai.com/v1
export SIDE_EYE_LLM_TOKEN=sk-...
export SIDE_EYE_LLM_MODEL=gpt-4o-mini
side-eye -llm https://github.com/example/project
```

## Environment

| Variable | Required | Meaning |
| --- | --- | --- |
| `SIDE_EYE_LLM_URL` | yes | OpenAI-compatible base URL. A base URL or the full completions URL both work |
| `SIDE_EYE_LLM_MODEL` | yes | Model name as the endpoint knows it |
| `SIDE_EYE_LLM_TOKEN` | no | Bearer token. Empty sends no `Authorization` header |
| `SIDE_EYE_LLM_PROMPT_FILE` | no | Path to a text file replacing the review prompt. Empty or missing keeps the default |
| `SIDE_EYE_LLM_TIMEOUT` | no | Wait for one review request, such as `90s` or `10m`. Default `5m` |

`-llm` with a missing URL or model prints an error and returns exit code 2.
Without `-llm`, side-eye never reads these variables and never calls the network.

## Timeout

A local model reading the whole execution surface takes minutes, and one still
loading its weights takes longer. Raise the wait rather than dropping `-llm`:

```bash
SIDE_EYE_LLM_TIMEOUT=10m side-eye -llm .
```

An empty or unusable value keeps the 5 minute default, so a typo cannot remove
the timeout.

## Privacy

The pass sends repository file content and paths to whatever endpoint
`SIDE_EYE_LLM_URL` names, so run it only with one you trust. Use a local server
when the content has to stay on the machine.

The token goes in the request header only, side-eye never prints it, and the
model output is parsed as data and never executed.