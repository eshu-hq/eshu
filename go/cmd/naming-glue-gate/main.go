// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command naming-glue-gate is documented in doc.go.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// defaultAPIKeyEnv names the environment variable holding the DeepSeek API
// key. Kept as a flag default (not a constant used directly) so a test or an
// alternate deployment can point at a different variable without a code
// change.
const defaultAPIKeyEnv = "DEEPSEEK_API_KEY" // #nosec G101 -- environment variable name, not a credential.

const defaultBaseURL = "https://api.deepseek.com"

const defaultModel = "deepseek-flash"

// defaultTimeoutSeconds bounds a single classification call by default. The
// only wired caller is pre-commit with -blocking=true (see
// scripts/verify-naming-glue-gate.sh), where a typical commit introduces a
// handful of new directories at most: a slow or blackholed network stalling
// an interactive `git commit` for tens of seconds before failing open
// anyway buys nothing, so this stays short. -timeout-seconds overrides it
// for a manual run against a much larger batch (e.g. auditing a long
// history range by hand).
const defaultTimeoutSeconds = 15

// Classifier is the subset of DeepSeekClient's contract run() depends on,
// so tests inject a fake instead of making a real API call.
type Classifier interface {
	Classify(ctx context.Context, candidates []Candidate) (Report, error)
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, nil, nil, os.Getenv))
}

// run implements the CLI. classifier and runner are nil in production,
// which real() below fills in from flags; tests pass fakes directly so
// nothing here touches the network or a real git repository.
func run(args []string, stdout, stderr io.Writer, classifier Classifier, runner GitRunner, getenv func(string) string) int {
	fs := flag.NewFlagSet("naming-glue-gate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoRoot := fs.String("repo-root", ".", "repository root to run git against")
	baseRef := fs.String("base-ref", "origin/main", "git ref to diff from")
	headRef := fs.String("head-ref", "HEAD", "git ref to diff to")
	dirsFlag := fs.String("dirs", "go/internal,go/cmd,go/pkg", "comma-separated repo-relative directories to scan for new subdirectories")
	blocking := fs.Bool("blocking", false, "exit 1 when a glued-compound directory is found (pre-commit); when false, findings are reported but the process always exits 0 (CI advisory mode)")
	apiKeyEnv := fs.String("api-key-env", defaultAPIKeyEnv, "environment variable holding the DeepSeek API key")
	model := fs.String("model", defaultModel, "DeepSeek model name")
	baseURL := fs.String("base-url", defaultBaseURL, "DeepSeek API base URL")
	timeoutSeconds := fs.Int("timeout-seconds", defaultTimeoutSeconds, "classification request timeout in seconds")
	exemptFile := fs.String("exempt-file", defaultExemptFile, "repo-relative exemption ledger of owner-approved glued-compound directory names, resolved against repo-root")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	dirs := strings.Split(*dirsFlag, ",")
	timeout := time.Duration(*timeoutSeconds) * time.Second

	if runner == nil {
		runner = NewGitRunner(*repoRoot)
	}

	candidates, err := NewDirectories(runner, *baseRef, *headRef, dirs)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "naming-glue-gate: cannot resolve new directories: %v\n", err)
		return 2
	}

	exemptPath := *exemptFile
	if !filepath.IsAbs(exemptPath) {
		exemptPath = filepath.Join(*repoRoot, exemptPath)
	}
	exempt, err := loadExemptPaths(exemptPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "naming-glue-gate: %v\n", err)
		return 2
	}
	candidates = filterExempt(candidates, exempt)
	if len(candidates) == 0 {
		_, _ = fmt.Fprintln(stderr, "naming-glue-gate: PASS (no newly introduced directory names to classify)")
		return 0
	}

	apiKey := getenv(*apiKeyEnv)
	if apiKey == "" {
		_, _ = fmt.Fprintf(stderr, "naming-glue-gate: %s is not set; skipping (advisory check only, not a naming failure)\n", *apiKeyEnv)
		return 0
	}

	if classifier == nil {
		classifier = &DeepSeekClient{
			HTTPClient: &http.Client{Timeout: timeout},
			BaseURL:    *baseURL,
			APIKey:     apiKey,
			Model:      *model,
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	report, err := classifier.Classify(ctx, candidates)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "naming-glue-gate: could not classify (%v); skipping this run rather than blocking on an infrastructure failure\n", err)
		return 0
	}

	report = applyDisposition(report, *blocking)
	if err := report.WriteJSON(stdout); err != nil {
		_, _ = fmt.Fprintf(stderr, "naming-glue-gate: writing JSON report: %v\n", err)
	}

	violations := report.Violations()
	if len(violations) == 0 {
		_, _ = fmt.Fprintf(stderr, "naming-glue-gate: PASS (%d candidate(s) reviewed, 0 glued compounds)\n", len(candidates))
		return 0
	}

	WriteHuman(stderr, violations)
	if *blocking {
		_, _ = fmt.Fprintf(stderr, "naming-glue-gate: %d glued-compound director%s found; nest instead of gluing (naming.md rule 3)\n", len(violations), plural(len(violations)))
		return 1
	}
	_, _ = fmt.Fprintf(stderr, "naming-glue-gate: %d glued-compound director%s found (advisory, non-blocking; would fail with -blocking)\n", len(violations), plural(len(violations)))
	return 0
}

func plural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
