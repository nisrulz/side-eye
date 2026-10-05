package scan

// detector reads one kind of surface and reports what runs code. A detector
// returns an error only when the target cannot be read at all, because that
// makes the whole scan untrustworthy.
type detector func(repo *repoLayout, add func(Finding)) error

// detectors are the surfaces side-eye covers. They are independent: each one
// looks at its own files and skips itself when the target has none of them.
var detectors = []detector{
	scanGit,
	scanVSCode,
	scanNodeJS,
	scanShell,
	scanAndroid,
}
