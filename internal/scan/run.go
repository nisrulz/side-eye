package scan

import (
	"flag"
	"fmt"
	"io"
	"os"
)

// runOptions carries the resolved flags, the target, and the streams to each
// scan runner.
type runOptions struct {
	target    string
	jsonOut   bool
	threshold Severity
	llm       llmConfig
	useLLM    bool
	ref       string
	token     string
	stdout    *os.File
	stderr    *os.File
}

// scanFlags holds the flag values exactly as the command line gave them.
type scanFlags struct {
	jsonOut bool
	ref     string
	token   string
	failOn  string
	useLLM  bool
}

func registerScanFlags(fs *flag.FlagSet) *scanFlags {
	f := &scanFlags{}
	fs.BoolVar(&f.jsonOut, "json", false, "print findings as JSON")
	fs.StringVar(&f.ref, "ref", "", "remote branch or tag to scan (URL scans)")
	fs.StringVar(&f.token, "token", "", "GitHub token for private repos (default GITHUB_TOKEN or GH_TOKEN)")
	fs.StringVar(&f.failOn, "fail-on", "high", "lowest severity that sets exit code 1: low, medium, high, critical, none")
	fs.BoolVar(&f.useLLM, "llm", false, "run an LLM review pass (needs SIDE_EYE_LLM_URL and SIDE_EYE_LLM_MODEL)")
	return f
}

// toOptions validates the flag values and resolves them into runOptions. It
// returns false after it has written the reason to stderr.
func (f *scanFlags) toOptions(target string, stdout, stderr *os.File) (runOptions, bool) {
	threshold, ok := parseFailOn(f.failOn)
	if !ok {
		failf(stderr, "Invalid -fail-on value %q", f.failOn)
		return runOptions{}, false
	}

	var llmCfg llmConfig
	if f.useLLM {
		var err error
		if llmCfg, err = loadLLMConfig(); err != nil {
			failf(stderr, "%v", err)
			return runOptions{}, false
		}
	}

	return runOptions{
		target:    target,
		jsonOut:   f.jsonOut,
		threshold: threshold,
		llm:       llmCfg,
		useLLM:    f.useLLM,
		ref:       f.ref,
		token:     resolveToken(f.token),
		stdout:    stdout,
		stderr:    stderr,
	}, true
}

func printUsage(fs *flag.FlagSet, stderr io.Writer) {
	fmt.Fprintln(stderr, "side-eye scans a git repository for code that runs on checkout, commit, or open.")
	fmt.Fprintln(stderr, "It reads files only and never executes git or any hook.")
	fmt.Fprintln(stderr)
	fmt.Fprintln(stderr, "Usage: side-eye [flags] [path | repo-url | zip-file]")
	fmt.Fprintln(stderr)
	fs.PrintDefaults()
}

// Run parses the flags, resolves the target, and dispatches to the matching
// scan runner. It returns the process exit code.
func Run(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("side-eye", flag.ContinueOnError)
	fs.SetOutput(stderr)
	flags := registerScanFlags(fs)
	fs.Usage = func() { printUsage(fs, stderr) }
	if err := fs.Parse(args); err != nil {
		return 2
	}

	setColor(stdout, flags.jsonOut)
	printBanner(stderr)

	target := "."
	if fs.NArg() > 0 {
		target = fs.Arg(0)
	}

	opts, ok := flags.toOptions(target, stdout, stderr)
	if !ok {
		return 2
	}

	switch {
	case isZipArg(opts.target):
		return runZip(opts)
	case isRemoteArg(opts.target):
		return runRemote(opts)
	default:
		return runLocal(opts)
	}
}

func runZip(opts runOptions) int {
	src, err := newZipSource(opts.target)
	if err != nil {
		failf(opts.stderr, "%v", err)
		return 2
	}
	defer src.Close()

	var findings []Finding
	add := func(f Finding) { findings = append(findings, f) }
	scanRemoteSource(src, add)
	src.scanGitDir(add)

	var notes []string
	if opts.useLLM {
		notes = append(notes, scanLLMSource(opts.llm, src, "", opts.stderr, add))
	}

	res := scanResult{
		jsonOut:  opts.jsonOut,
		heading:  "Zip scan",
		root:     src.display,
		findings: findings,
		notes:    notes,
		advice:   func(w io.Writer) { printZipAdvice(w, src.hasGitDir()) },
	}
	if !printResult(opts.stdout, opts.stderr, res) {
		return 2
	}
	return exitCodeFor(findings, opts.threshold)
}

func runLocal(opts runOptions) int {
	repo, findings, err := scanRepo(opts.target)
	if err != nil {
		failf(opts.stderr, "%v", err)
		return 2
	}
	res := scanResult{
		jsonOut:  opts.jsonOut,
		root:     repo.root,
		findings: findings,
	}
	if repo.plain {
		res.heading = "Directory scan"
		res.advice = func(w io.Writer) { printPlainAdvice(w) }
	}
	if opts.useLLM {
		add := func(f Finding) { findings = append(findings, f) }
		res.notes = append(res.notes,
			scanLLMSource(opts.llm, newLocalSource(repo), localHooksDir(repo), opts.stderr, add))
		res.findings = findings
	}
	if !printResult(opts.stdout, opts.stderr, res) {
		return 2
	}
	return exitCodeFor(findings, opts.threshold)
}

func runRemote(opts runOptions) int {
	rt, err := parseRemoteURL(opts.target, opts.ref, opts.token)
	if err != nil {
		failf(opts.stderr, "%v", err)
		return 2
	}

	src, findings, err := scanRemote(rt)
	if err != nil {
		failf(opts.stderr, "Remote scan failed for %s: %v", rt.display(), err)
		fmt.Fprintln(opts.stderr, dim("→ Clone without a working tree and scan locally instead:"))
		printCommand(opts.stderr, "git clone --no-checkout %s %s && side-eye %s", rt.clone, rt.repo, rt.repo)
		return 2
	}
	res := scanResult{
		jsonOut:  opts.jsonOut,
		heading:  "Remote scan",
		root:     rt.display(),
		findings: findings,
		remote:   rt,
		advice:   func(w io.Writer) { printCloneAdvice(w, rt) },
	}
	if opts.useLLM {
		add := func(f Finding) { findings = append(findings, f) }
		res.notes = append(res.notes, scanLLMSource(opts.llm, src, "", opts.stderr, add))
		res.findings = findings
	}
	if !printResult(opts.stdout, opts.stderr, res) {
		return 2
	}
	return exitCodeFor(findings, opts.threshold)
}

func resolveToken(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if v := os.Getenv("GITHUB_TOKEN"); v != "" {
		return v
	}
	return os.Getenv("GH_TOKEN")
}
