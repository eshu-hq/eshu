// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command dirgate is documented in doc.go.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, os.Getenv))
}

// Exit statuses. An unresolvable base is never a pass.
const (
	exitGreen  = 0
	exitRed    = 1
	exitBroken = 2
)

// run is the testable CLI. It returns the process exit status.
func run(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	fs := flag.NewFlagSet("dirgate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repo := fs.String("repo", ".", "repository root")
	baseRef := fs.String("base-ref", "", "base ref for pre-push and pre-commit (default $ESHU_DIRGATE_BASE, else "+defaultBaseRef+")")
	event := fs.String("event", "", "CI event name for the ci mode (default $GITHUB_EVENT_NAME)")
	outDir := fs.String("out", "", "baseline mode: directory for the two TSVs (default go/cmd/dirgate; - prints to stdout)")
	baselineDir := fs.String("baseline-dir", "", "read the baselines from this directory instead of the head tree (replay only; skips the write-once check)")
	headRef := fs.String("head-ref", "HEAD", "commit the ci mode checks against its first parent (replay and debugging)")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "usage: dirgate [-repo dir] [-base-ref ref] [-event name] [-head-ref ref] ci|pre-push|pre-commit [staged files...] | baseline")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return exitBroken
	}
	if fs.NArg() == 0 {
		fs.Usage()
		return exitBroken
	}
	mode, files := fs.Arg(0), fs.Args()[1:]
	// Go stops flag parsing at the first non-flag argument, so anything after the
	// mode would be silently ignored. Only pre-commit takes arguments after it.
	if mode != modePreCommit && len(files) > 0 {
		_, _ = fmt.Fprintf(stderr, "dirgate: unexpected arguments after the %s mode: %v (flags go before the mode)\n", mode, files)
		fs.Usage()
		return exitBroken
	}
	if *baseRef == "" {
		*baseRef = getenv("ESHU_DIRGATE_BASE")
	}
	if *baseRef == "" {
		*baseRef = defaultBaseRef
	}
	if *event == "" {
		*event = getenv("GITHUB_EVENT_NAME")
	}

	g := gitRepo{dir: *repo}
	if mode == modeBaseline {
		return runBaseline(g, *baseRef, *outDir, stdout, stderr)
	}
	var (
		res resolution
		err error
	)
	switch mode {
	case modeCI:
		res, err = resolveCI(g, *event, *headRef)
	case modePrePush:
		res, err = resolvePrePush(g, *baseRef)
	case modePreCommit:
		res, err = resolvePreCommit(g, *baseRef, files)
	default:
		_, _ = fmt.Fprintf(stderr, "dirgate: unknown mode %q\n", mode)
		fs.Usage()
		return exitBroken
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "dirgate: cannot resolve the comparison: %v\n", err)
		return exitBroken
	}
	return check(g, res, len(files), *baselineDir, stdout, stderr)
}

// check loads both trees, applies the rule, and prints the verdict.
func check(g gitRepo, res resolution, nPaths int, baselineDir string, stdout, stderr io.Writer) int {
	fail := func(err error) int {
		_, _ = fmt.Fprintf(stderr, "dirgate: %v\n", err)
		return exitBroken
	}
	base, err := loadSnapshot(g, res.baseCommit)
	if err != nil {
		return fail(err)
	}
	head, err := loadSnapshot(g, res.headTree)
	if err != nil {
		return fail(err)
	}
	trailers, err := loadTrailers(g, res.revRange)
	if err != nil {
		return fail(err)
	}
	history, err := loadHistoryWaivers(g, res.baseCommit)
	if err != nil {
		return fail(err)
	}
	headBaseline, err := loadBaselines(g, res.headTree, baselineDir)
	if err != nil {
		return fail(err)
	}
	markers, err := loadMarkers(g, res.headTree)
	if err != nil {
		return fail(err)
	}
	findings, checked := evaluate(evalInput{
		base: base, head: head, trailers: trailers, history: history,
		baseline: headBaseline, markers: markers, only: res.only,
	})
	if baselineDir == "" {
		baseBaseline, err := loadBaselines(g, res.baseCommit, "")
		if err != nil {
			return fail(err)
		}
		findings = append(findings, writeOnceFindings(baseBaseline, headBaseline)...)
	}
	total := 0
	for _, n := range history {
		total += n
	}
	return report(stdout, res, findings, checked, nPaths, total)
}

// runBaseline writes the two baseline TSVs for the tree at ref. The output is
// deterministic, so landing and rebasing regenerate identical bytes.
func runBaseline(g gitRepo, ref, outDir string, stdout, stderr io.Writer) int {
	commit, err := g.resolveCommit(ref)
	if err != nil || commit == "" {
		_, _ = fmt.Fprintf(stderr, "dirgate: baseline ref %q does not resolve to a commit\n", ref)
		return exitBroken
	}
	snap, err := loadSnapshot(g, commit)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "dirgate: %v\n", err)
		return exitBroken
	}
	capTSV, namingTSV := generateBaselines(snap)
	if outDir == "-" {
		_, _ = fmt.Fprintf(stdout, "== %s\n%s== %s\n%s", baselineCapName, capTSV, baselineNamingName, namingTSV)
		return exitGreen
	}
	if outDir == "" {
		outDir = filepath.Join(g.dir, filepath.FromSlash(path.Dir(baselineCapFile)))
	}
	for name, content := range map[string]string{baselineCapName: capTSV, baselineNamingName: namingTSV} {
		if err := os.MkdirAll(outDir, 0o755); err != nil { // #nosec G301 -- repository data directory, checked-in layout uses 0755
			_, _ = fmt.Fprintf(stderr, "dirgate: %v\n", err)
			return exitBroken
		}
		if err := os.WriteFile(filepath.Join(outDir, name), []byte(content), 0o644); err != nil { // #nosec G306 -- repository data files, not secrets
			_, _ = fmt.Fprintf(stderr, "dirgate: write %s: %v\n", name, err)
			return exitBroken
		}
	}
	_, _ = fmt.Fprintf(stdout, "dirgate: wrote %s and %s to %s from %s (%d directories over the cap, %d naming rows)\n",
		baselineCapName, baselineNamingName, outDir, short(commit), len(tsvRows(capTSV)), len(tsvRows(namingTSV)))
	return exitGreen
}
