# Config rules

The rule engine inspects git config keys that make git run an external command or read a hook from an unexpected location.

## Flow

1. `parseGitConfigTree` in `config.go` returns `[]gitConfigEntry`.
   It follows `include.path` and `includeIf.*.path` up to depth 5, so keys in an included file are still inspected.
2. `evalConfig` in `git_config_rules.go` lowercases the section, key, and subsection.
3. `sectionHandlers` maps each section to a handler.
4. The handler appends findings through `commandFinding`.

`commandFinding` appends the config value to the detail and truncates it to 120 bytes.

## Severity levels

| Level | Meaning |
| --- | --- |
| Critical | Git runs the value as a command (hooks path, filter, alias) |
| High | Git runs the value, or the config permits a risky transport |
| Medium | The config weakens safety (extra attributes file, URL rewrite) |

See [reporting.md](reporting.md) for the full severity scale.

## Handlers

| Section | Handler | Keys and severity |
| --- | --- | --- |
| `core` | `evalCore` | `hooksPath` critical, `fsmonitor` critical, `sshCommand` critical, `editor`/`askpass`/`gitProxy`/`pager` high, `attributesFile` medium |
| `filter` | `evalFilter` | `clean`, `smudge`, `process` critical |
| `diff` | `evalDiff` | `external` critical, `command`, `textconv` critical |
| `merge` | `evalMerge` | `driver` critical |
| `credential` | `evalCredential` | `helper` |
| `alias` | `evalAlias` | a value that starts with `!` is critical |
| `submodule` | `evalSubmodule` | `update` with `!` critical, `url` with `ext::` critical, `url` with `-` high |
| `protocol` | `evalProtocol` | `allow` for `ext` or `file` high |
| `include`, `includeif` | `evalInclude` | `path` high |
| `interactive` | `evalInteractive` | `diffFilter` critical |
| `gpg` | `evalGpg` | `program` high |
| `sequence` | `evalSequence` | `editor` high |
| `tar` | `evalTar` | `command` high |
| `url` | `evalURL` | `insteadOf` medium |

`core.fsmonitor` is skipped when the value is empty or `true`, because that value enables the built-in monitor.

## Allowlists

`evalCredential` skips builtin helpers listed in `builtinCredentialHelpers`.
It flags a value that starts with `!`, and a value that looks like a path (it contains `/`, `\`, or a space, or ends in `.exe`).

`builtinMergeDrivers` in `git_detector.go` lists the safe merge drivers.

## Add a rule

1. Add or extend a handler in `git_config_rules.go`.
2. Register a new section in `sectionHandlers`.
3. Add a test in `scan_test.go`.

Example handler that flags `core.foo`:

```go
func evalCore(e gitConfigEntry, k configKey, add func(Finding)) {
	switch k.key {
	// existing cases ...
	case "foo":
		add(commandFinding(e, SeverityHigh, "core.foo",
			"Git runs this command"))
	}
}
```

Keep the finding title equal to the config key. The tests match findings by title.
