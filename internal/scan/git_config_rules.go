package scan

// The config rule engine. Each handler covers one git config section and reports the
// keys that make git run an external command or read a hook from an unexpected place.

import (
	"strings"
)

// configKey is a lowercased git config key for rule lookup.
type configKey struct {
	section string
	key     string
	sub     string
}

// evalConfig inspects git config keys that make git run an external command or
// read a hook from an unexpected place.
func evalConfig(entries []gitConfigEntry, add func(Finding)) {
	for _, e := range entries {
		k := configKey{
			section: strings.ToLower(e.Section),
			key:     strings.ToLower(e.Key),
			sub:     strings.ToLower(e.Subsection),
		}
		if handler, ok := sectionHandlers[k.section]; ok {
			handler(e, k, add)
		}
	}
}

var sectionHandlers = map[string]func(gitConfigEntry, configKey, func(Finding)){
	"core":        evalCore,
	"filter":      evalFilter,
	"diff":        evalDiff,
	"merge":       evalMerge,
	"credential":  evalCredential,
	"alias":       evalAlias,
	"submodule":   evalSubmodule,
	"protocol":    evalProtocol,
	"include":     evalInclude,
	"includeif":   evalInclude,
	"interactive": evalInteractive,
	"gpg":         evalGpg,
	"sequence":    evalSequence,
	"tar":         evalTar,
	"url":         evalURL,
}

func evalCore(e gitConfigEntry, k configKey, add func(Finding)) {
	switch k.key {
	case "hookspath":
		add(commandFinding(e, SeverityCritical, "core.hooksPath",
			"Git reads hooks from an unexpected directory"))
	case "fsmonitor":
		v := strings.TrimSpace(e.Value)
		if v == "" || strings.EqualFold(v, "true") {
			return
		}
		add(commandFinding(e, SeverityCritical, "core.fsmonitor",
			"Git runs this filesystem monitor program on every git command"))
	case "sshcommand":
		add(commandFinding(e, SeverityCritical, "core.sshCommand",
			"Git runs this command for ssh transport"))
	case "editor", "askpass", "gitproxy", "pager":
		add(commandFinding(e, SeverityHigh, "core."+k.key,
			"Git runs this command through the shell"))
	case "attributesfile":
		add(commandFinding(e, SeverityMedium, "core.attributesFile",
			"Git reads attributes from a file outside the working tree"))
	}
}

func evalFilter(e gitConfigEntry, k configKey, add func(Finding)) {
	if k.key != "clean" && k.key != "smudge" && k.key != "process" {
		return
	}
	add(commandFinding(e, SeverityCritical, "filter."+k.sub+"."+k.key,
		"Git runs this filter command on checkout and add"))
}

func evalDiff(e gitConfigEntry, k configKey, add func(Finding)) {
	if k.key == "external" {
		add(commandFinding(e, SeverityCritical, "diff.external",
			"Git runs this command for every diff"))
		return
	}
	if k.key == "command" || k.key == "textconv" {
		add(commandFinding(e, SeverityCritical, "diff."+k.sub+"."+k.key,
			"Git runs this diff driver command"))
	}
}

func evalMerge(e gitConfigEntry, k configKey, add func(Finding)) {
	if k.key == "driver" && k.sub != "" {
		add(commandFinding(e, SeverityCritical, "merge."+k.sub+".driver",
			"Git runs this merge driver during merges"))
	}
}

func evalCredential(e gitConfigEntry, k configKey, add func(Finding)) {
	if k.key != "helper" {
		return
	}
	v := strings.TrimSpace(e.Value)
	if strings.HasPrefix(v, "!") {
		add(commandFinding(e, SeverityCritical, "credential.helper",
			"Git runs this shell command to handle credentials"))
		return
	}
	if v == "" || builtinCredentialHelpers[strings.ToLower(v)] {
		return
	}
	if strings.ContainsAny(v, "/\\ ") || strings.Contains(v, ".exe") {
		add(commandFinding(e, SeverityHigh, "credential.helper",
			"Git runs this external credential helper: "+v))
	}
}

func evalAlias(e gitConfigEntry, k configKey, add func(Finding)) {
	if strings.HasPrefix(strings.TrimSpace(e.Value), "!") {
		add(commandFinding(e, SeverityCritical, "alias."+k.key,
			"Git alias runs a shell command"))
	}
}

func evalSubmodule(e gitConfigEntry, k configKey, add func(Finding)) {
	value := strings.TrimSpace(e.Value)
	switch k.key {
	case "update":
		if strings.HasPrefix(value, "!") {
			add(commandFinding(e, SeverityCritical, "submodule."+k.sub+".update",
				"Git runs this command when the submodule updates"))
		}
	case "url":
		if strings.HasPrefix(value, "ext::") {
			add(commandFinding(e, SeverityCritical, "submodule url",
				"Git runs this ext:: transport command on submodule update"))
		} else if strings.HasPrefix(value, "-") {
			add(commandFinding(e, SeverityHigh, "submodule url",
				"Submodule url starts with a dash and can inject git options"))
		}
	}
}

func evalProtocol(e gitConfigEntry, k configKey, add func(Finding)) {
	if k.key == "allow" && (k.sub == "ext" || k.sub == "file") {
		add(commandFinding(e, SeverityHigh, "protocol."+k.sub+".allow",
			"Git permits the "+k.sub+" transport, which can run commands"))
	}
}

func evalInclude(e gitConfigEntry, k configKey, add func(Finding)) {
	if k.key == "path" {
		add(commandFinding(e, SeverityHigh, k.section+".path",
			"Git loads extra config from this file; inspect the target"))
	}
}

func evalInteractive(e gitConfigEntry, k configKey, add func(Finding)) {
	if k.key == "difffilter" {
		add(commandFinding(e, SeverityCritical, "interactive.diffFilter",
			"Git runs this command for interactive add"))
	}
}

func evalGpg(e gitConfigEntry, k configKey, add func(Finding)) {
	if k.key == "program" {
		add(commandFinding(e, SeverityHigh, "gpg.program",
			"Git runs this program for signing and verification"))
	}
}

func evalSequence(e gitConfigEntry, k configKey, add func(Finding)) {
	if k.key == "editor" {
		add(commandFinding(e, SeverityHigh, "sequence.editor",
			"Git runs this editor during interactive rebase"))
	}
}

func evalTar(e gitConfigEntry, k configKey, add func(Finding)) {
	if k.key == "command" {
		add(commandFinding(e, SeverityHigh, "tar."+k.sub+".command",
			"Git runs this command when creating archives"))
	}
}

func evalURL(e gitConfigEntry, k configKey, add func(Finding)) {
	if k.key == "insteadof" {
		add(commandFinding(e, SeverityMedium, "url."+k.sub+".insteadOf",
			"Git rewrites clone and fetch URLs"))
	}
}

var builtinCredentialHelpers = map[string]bool{
	"cache": true, "store": true, "osxkeychain": true, "libsecret": true,
	"wincred": true, "manager": true, "manager-core": true, "gnome-keyring": true,
	"store--file": true,
}

func commandFinding(e gitConfigEntry, sev Severity, title, detail string) Finding {
	if name := strings.TrimSpace(e.Value); name != "" {
		detail = detail + ": " + truncate(name, 120)
	}
	return Finding{Severity: sev, Path: e.Path, Line: e.Line, Title: title, Detail: detail}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
