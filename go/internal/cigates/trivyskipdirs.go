// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Paths of the two artifacts that must agree on which directories the Trivy
// filesystem scan skips. CI is authoritative; the local wrapper mirrors it.
const (
	trivyLocalScriptPath = "scripts/dev/trivy-fs-local.sh"
	trivyCIWorkflowPath  = ".github/workflows/security-scan.yml"
)

var (
	// trivyLocalSkipDirsRE captures the local wrapper's skip_dirs assignment.
	trivyLocalSkipDirsRE = regexp.MustCompile(`(?m)^skip_dirs="([^"]*)"`)
	// trivyCISkipDirsRE captures the CI action input. security-scan.yml has
	// exactly one such input (trivy-image passes no skip-dirs), and the
	// arity check below refuses to guess if that ever stops being true.
	// The whitespace classes are [ \t], not \s: \s matches a newline, and a
	// "skip-dirs:" key with no same-line value must fail the arity check
	// rather than have the gap silently bridge into the next line and
	// capture an unrelated token as the value. The captured value may still
	// carry a surrounding quote pair (a quoted YAML scalar); stripCIQuoting
	// removes it before the value is compared.
	trivyCISkipDirsRE = regexp.MustCompile(`(?m)^[ \t]*skip-dirs:[ \t]*(\S+)[ \t]*$`)
)

// checkTrivySkipDirsParity validates that scripts/dev/trivy-fs-local.sh skips
// exactly the directories .github/workflows/security-scan.yml's trivy-fs job
// skips.
//
// The local wrapper's own comment promises "the same skip-dirs ... so local
// findings match CI rather than reporting noise CI suppresses", and that
// promise is load-bearing rather than decorative: the two lists are separate
// string literals in different languages with no shared source, so nothing but
// this check couples them. They had already drifted -- the local list omitted
// go/cmd/mock-oidc-idp, so every `make pre-pr` reported the trivy-fs gate as
// exit 1 on the mock IdP's committed synthetic RSA key (#4971), a finding CI
// deliberately suppresses. A security gate that is always red teaches everyone
// to ignore its exit code, which is how a real finding eventually gets waved
// through.
//
// Order is not compared. `--skip-dirs` is an unordered list, so requiring
// identical ordering would fail on a harmless reorder while proving nothing;
// the sets are compared and the message names the symmetric difference.
// Entries are compared as literal strings, not trimmed: trivy's --skip-dirs is
// a pflag string slice that comma-splits its argument but does not trim it, so
// a padded entry ("secretdir ") and an unpadded one ("secretdir") are
// different directory patterns to trivy itself (verified against trivy
// 0.72.0). Normalizing that away would make this check pass while trivy skips
// different things locally, which is the exact false green it exists to catch.
//
// Both directions are errors, and they fail differently. A directory CI skips
// but local does not produces local-only noise (the drift above). A directory
// local skips but CI does not is worse: the local gate goes green on a finding
// CI will flag, so the drift is discovered in CI instead of before the push.
func checkTrivySkipDirsParity(repoRoot string) []error {
	localPath := filepath.Join(repoRoot, trivyLocalScriptPath)
	ciPath := filepath.Join(repoRoot, trivyCIWorkflowPath)
	localExists, ciExists := regularFileExists(localPath), regularFileExists(ciPath)

	switch {
	case !localExists && !ciExists:
		// Neither artifact is present, so there is no parity to check. This is
		// the shape of this package's synthetic drift fixtures, which build a
		// minimal repo out of a pre-commit config and a workflow or two; the
		// real repo always has both. Reporting "cannot read" there would fail
		// every fixture on the absence of a file the fixture never claimed to
		// have.
		return nil
	case localExists != ciExists:
		// Exactly one side exists. That is drift in its own right: either the
		// local wrapper lost the CI job it mirrors, or CI gained a scan with no
		// local counterpart. Skipping here would hide the very asymmetry this
		// check exists to catch.
		present, missing := trivyLocalScriptPath, trivyCIWorkflowPath
		if ciExists {
			present, missing = trivyCIWorkflowPath, trivyLocalScriptPath
		}
		return []error{fmt.Errorf(
			"drift: %s exists but %s does not -- the local trivy-fs wrapper and its CI job must "+
				"exist together so their skip-dirs can be kept in parity", present, missing,
		)}
	}

	local, err := readSingleCapture(localPath, trivyLocalSkipDirsRE, `skip_dirs="..."`)
	if err != nil {
		return []error{err}
	}
	ci, err := readSingleCapture(ciPath, trivyCISkipDirsRE, "skip-dirs:")
	if err != nil {
		return []error{err}
	}
	ci = stripCIQuoting(ci)

	onlyCI := skipDirsDifference(ci, local)
	onlyLocal := skipDirsDifference(local, ci)
	if len(onlyCI) == 0 && len(onlyLocal) == 0 {
		return nil
	}

	var parts []string
	if len(onlyCI) > 0 {
		parts = append(parts, fmt.Sprintf(
			"CI skips %q which %s does not (local-only noise: the gate reports findings CI suppresses)",
			onlyCI, trivyLocalScriptPath,
		))
	}
	if len(onlyLocal) > 0 {
		parts = append(parts, fmt.Sprintf(
			"%s skips %q which CI does not (local blind spot: the gate passes on findings CI will flag)",
			trivyLocalScriptPath, onlyLocal,
		))
	}
	return []error{fmt.Errorf(
		"drift: trivy-fs skip-dirs disagree between %s and %s -- %s; CI is authoritative, so update the local script to match",
		trivyLocalScriptPath, trivyCIWorkflowPath, strings.Join(parts, "; "),
	)}
}

// stripCIQuoting removes a single matched pair of surrounding double or
// single quotes from a YAML scalar value. security-scan.yml's skip-dirs value
// may be quoted ("a,b" or 'a,b'); trivyCISkipDirsRE's (\S+) capture cannot
// distinguish a quote character from a directory character, so a quoted value
// would otherwise carry its quotes into the comma-split entries, fabricating
// a bogus directory name at each end of the list and reporting drift for what
// is really just a quoting-style change. A value with no matching quote pair
// is returned unchanged.
func stripCIQuoting(s string) string {
	if len(s) >= 2 {
		first, last := s[0], s[len(s)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// regularFileExists reports whether path is an existing regular file. A
// directory or an unreadable entry counts as absent: the parity check wants a
// file it can parse, and treating anything else as present would turn a
// stat quirk into a spurious drift error.
func regularFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// readSingleCapture returns re's first submatch in the file at path, requiring
// exactly one match. Zero matches means the marker moved or was renamed; more
// than one means the file grew a second declaration and the check can no longer
// tell which one governs. Both are reported rather than skipped -- a parity
// check that silently passes when it cannot find its inputs is worse than none,
// because the green result reads as proof of agreement.
func readSingleCapture(path string, re *regexp.Regexp, marker string) (string, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- repo-relative, caller-constructed path
	if err != nil {
		return "", fmt.Errorf("trivy skip-dirs parity: cannot read %s: %w", path, err)
	}
	matches := re.FindAllStringSubmatch(string(raw), -1)
	if len(matches) != 1 {
		return "", fmt.Errorf(
			"trivy skip-dirs parity: found %d %s declarations in %s, want exactly 1 -- "+
				"the parity check cannot tell which one governs; restore a single declaration or update the check",
			len(matches), marker, path,
		)
	}
	return matches[0][1], nil
}

// skipDirsDifference returns the entries present in a but not in b, comparing
// the comma-separated lists as sets. Entries are compared as literal strings,
// NOT trimmed: trivy's --skip-dirs is a pflag string slice that comma-splits
// its argument but does not trim it, so whitespace is part of the directory
// pattern rather than incidental formatting -- trimming here would make a
// padded local list compare equal to an unpadded CI list while trivy skips
// different things locally. The only entries dropped are genuinely empty
// ones, which a trailing or doubled comma produces and trivy itself tolerates
// as harmless (verified against trivy 0.72.0).
func skipDirsDifference(a, b string) []string {
	inB := make(map[string]struct{})
	for _, dir := range strings.Split(b, ",") {
		if dir != "" {
			inB[dir] = struct{}{}
		}
	}

	var diff []string
	seen := make(map[string]struct{})
	for _, dir := range strings.Split(a, ",") {
		if dir == "" {
			continue
		}
		if _, ok := inB[dir]; ok {
			continue
		}
		if _, dup := seen[dir]; dup {
			continue
		}
		seen[dir] = struct{}{}
		diff = append(diff, dir)
	}
	sort.Strings(diff)
	return diff
}
