// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"fmt"
	"path"
	"strings"
)

// goPackageSubcommands are the `go` subcommands whose `./pkg` arguments name
// code that the gate compiles, runs, or executes directives in. `go list` is
// deliberately absent: it reports on packages without building or running them,
// so a gate that only lists a package is not made false-green by a change
// inside it. `generate` IS present — it executes the package's directives, so a
// change there can change the gate's verdict. It adds nothing to today's
// registry (sdk-go-factschema's `go generate ./...` runs in a directory its own
// `go build ./...` already registers), which is the point: the class is closed
// before a gate relies on it.
var goPackageSubcommands = map[string]struct{}{
	"build":    {},
	"generate": {},
	"run":      {},
	"test":     {},
	"vet":      {},
}

// argTrimCutset strips shell punctuation that can adhere to a token once a
// command is split on whitespace: quotes, and the parentheses of a subshell.
// A package argument that closes a subshell arrives as "./internal/x)", and
// without the ")" the derived directory is "go/internal/x)" — a path no trigger
// can match, so the gate is reported as uncovered when it is in fact fine.
const argTrimCutset = `"'()`

// shellSeparators end the argument run of a single command inside a compound
// shell line, so a `./pkg` token after one of them does not belong to the
// preceding `go` invocation.
var shellSeparators = map[string]struct{}{
	"&&": {}, "||": {}, "|": {}, ";": {}, "&": {},
}

// checkGoPackageTriggerCoverage reports every gate whose own local command
// compiles or runs a Go package that none of that gate's triggers matches.
//
// It is the Go-language half of the self-trigger property that
// checkScriptTriggerCoverage (scripttrigger.go) enforces for shell gates, and
// it exists for the same reason: a gate that does not select on an edit to its
// own implementation is false-green locally, and CI becomes the first place the
// edit runs. 19 of the registry's local gates are implemented as a Go package
// (`go run ./cmd/x`, `go test ./internal/y`) rather than a scripts/ file, and
// checkScriptTriggerCoverage cannot see any of them — it only derives
// "scripts/"-prefixed tokens. Editing the program that IS the gate went
// unchecked for exactly the gates whose logic is hardest to review by eye
// (#5873).
//
// The package set is derived, not declared, so a gate added later inherits the
// property instead of having to remember it.
//
// Narrowings, all deliberate:
//
//   - Only the four subcommands in goPackageSubcommands count. `go list -deps
//     ./...` in the sdk gates reports on packages without running them, so a
//     change inside a merely-listed package cannot alter the gate's verdict.
//   - A `./pkg` token is attributed to the most recent `go <subcommand>` on the
//     same command, and the run ends at the next shell separator. A bare
//     `./pkg` with no preceding go subcommand (none in the registry today) is
//     ignored rather than guessed at.
//   - `cd` and `go -C <dir>` are both honoured, wherever they appear rather
//     than only at the start, because ci-gate-registry reaches its own package
//     through a mid-command subshell (`... && (cd go && go test
//     ./internal/cigates -count=1)`). The directory is tracked forward through
//     the whole command; shell scoping of a subshell `cd` is NOT modelled,
//     which can only ever attribute a package to a deeper directory than the
//     shell would use. That direction is safe: it demands a more specific
//     trigger than strictly necessary, so it cannot manufacture a false green.
//   - A package argument that resolves outside the repository, or to the
//     repository root itself, is skipped rather than reported. Neither names an
//     in-repo file whose edit could go unselected.
//
// Two narrowings were found by probing this parser rather than reading it, and
// both were silent rather than loud, which is the shape #5934 spent a round
// removing from the scripts-side check:
//
//   - A `go` subcommand outside goPackageSubcommands yields nothing. That is
//     correct for `go list` (see above) but was NOT correct for `go generate`,
//     which executes the package; it is in the set now. Anything else new —
//     `go tool`, say — is silently invisible until it is added, so add it when
//     a gate starts using it rather than assuming this set is closed.
//   - Shell punctuation adhering to a token (quotes, subshell parentheses) used
//     to travel into the derived path, so a package argument closing a subshell
//     produced the unmatchable directory "go/internal/x)". argTrimCutset strips
//     it. This never reported a false GREEN — it reported a false finding — but
//     it would have sent a reader hunting for a trigger that could not exist.
//
// Recursive package specs (`./...`, `./cmd/...`) require a trigger that matches
// nested files too, since that is the file set the command actually compiles.
func checkGoPackageTriggerCoverage(reg *Registry) []error {
	var errs []error
	for _, g := range reg.Gates {
		if g.Local == nil {
			continue
		}
		for _, c := range []struct{ field, command string }{
			{"local.command", g.Local.Command},
			{"local.test_command", g.Local.TestCommand},
		} {
			if c.command == "" {
				continue
			}
			for _, pkg := range extractGoPackageDirs(c.command) {
				if triggerCoversPackage(g.Triggers, pkg) {
					continue
				}
				errs = append(errs, fmt.Errorf(
					"drift: gate %q %s runs Go package %q but no trigger of that gate matches %s "+
						"(triggers: %v); a PR editing only that package would not select the gate "+
						"locally and would first fail in CI — add %q as a trigger",
					g.ID, c.field, pkg.dir, pkg.describe(), g.Triggers, pkg.dir+"/**",
				))
			}
		}
	}
	return errs
}

// goPackageDir is one repo-relative package directory a gate's command builds,
// runs, or tests, plus whether the command reached it recursively.
type goPackageDir struct {
	dir       string
	recursive bool
}

// describe renders the file set the command compiles, for the drift message.
func (p goPackageDir) describe() string {
	if p.recursive {
		return "files under " + p.dir + "/ at any depth"
	}
	return "files directly in " + p.dir + "/"
}

// probes returns the synthetic paths a trigger must match to cover the package.
// A non-recursive package needs only a file sitting directly in the directory;
// a recursive one also needs a file in a nested subdirectory, because that is
// what `./...` compiles. The names are arbitrary — only the shape of the path
// matters to MatchGlob — but they end in .go so a trigger legitimately narrowed
// to Go sources (`go/internal/ifa/*.go`) still counts as covering.
func (p goPackageDir) probes() []string {
	direct := path.Join(p.dir, "zz_selftrigger_probe.go")
	if !p.recursive {
		return []string{direct}
	}
	return []string{direct, path.Join(p.dir, "zz_selftrigger_probe", "nested.go")}
}

// triggerCoversPackage reports whether a single trigger matches every probe the
// package needs. One trigger must cover them all: two triggers that each match a
// different depth do not prove any single glob spans the package.
func triggerCoversPackage(triggers []string, pkg goPackageDir) bool {
	probes := pkg.probes()
	for _, t := range triggers {
		all := true
		for _, probe := range probes {
			if !MatchGlob(t, probe) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

// extractGoPackageDirs returns the repo-relative Go package directories a shell
// command compiles or runs, deduplicated and in first-seen order. It tracks the
// working directory across `cd` tokens so a `./pkg` argument resolves the way
// the shell would resolve it.
func extractGoPackageDirs(command string) []goPackageDir {
	fields := strings.Fields(command)
	cwd := ""
	inGoCommand := false
	inGoPackageArgs := false
	seen := make(map[goPackageDir]struct{}, len(fields))
	var out []goPackageDir

	for i := 0; i < len(fields); i++ {
		w := strings.Trim(fields[i], argTrimCutset)

		if _, isSep := shellSeparators[w]; isSep {
			inGoCommand = false
			inGoPackageArgs = false
			continue
		}

		if w == "cd" && i+1 < len(fields) {
			cwd = joinRepoRel(cwd, strings.Trim(fields[i+1], argTrimCutset))
			i++
			inGoCommand = false
			inGoPackageArgs = false
			continue
		}

		// `go -C <dir>` changes the directory package arguments resolve against
		// exactly as `cd` does, and go accepts it either before or after the
		// subcommand (verified against the pinned toolchain, go1.26.5: both
		// `go -C go list ./internal/cigates` and `go list -C go
		// ./internal/cigates` resolve the package). It is honoured only inside
		// a `go` command so that an unrelated tool's `-C` — `make -C go ...` —
		// cannot move the directory the shell never left.
		if inGoCommand && w == "-C" && i+1 < len(fields) {
			cwd = joinRepoRel(cwd, strings.Trim(fields[i+1], argTrimCutset))
			i++
			continue
		}

		if w == "go" {
			inGoCommand = true
			inGoPackageArgs = false
			continue
		}

		// The subcommand is recognised wherever it lands in the go command
		// rather than only immediately after the `go` word, since `-C <dir>`
		// may precede it. Nothing is consumed speculatively: a bare directory
		// argument spelled "go" used to swallow the package argument after it,
		// which is a SILENT skip of the exact kind #5934 removed from the
		// scripts-side check.
		if inGoCommand && !inGoPackageArgs {
			if _, ok := goPackageSubcommands[w]; ok {
				inGoPackageArgs = true
				continue
			}
		}

		if !inGoPackageArgs || !strings.HasPrefix(w, "./") {
			continue
		}

		pkg, ok := resolvePackageArg(cwd, w)
		if !ok {
			continue
		}
		if _, dup := seen[pkg]; dup {
			continue
		}
		seen[pkg] = struct{}{}
		out = append(out, pkg)
	}
	return out
}

// resolvePackageArg turns a `./pkg` or `./pkg/...` argument into a
// repo-relative package directory rooted at cwd. It reports false when the
// argument escapes the repository root, which names no in-repo file.
func resolvePackageArg(cwd, arg string) (goPackageDir, bool) {
	recursive := false
	rel := strings.TrimPrefix(arg, "./")
	if rel == "..." || strings.HasSuffix(rel, "/...") {
		recursive = true
		rel = strings.TrimSuffix(strings.TrimSuffix(rel, "..."), "/")
	}

	dir := joinRepoRel(cwd, rel)
	// "" and "." are the repository root: every path is inside it, so the
	// self-trigger property is vacuous and there is nothing to assert. A dir of
	// ".." or one under "../" escaped the repo. The escape test is written
	// exactly, not as a ".." prefix, so a repo-root directory whose name merely
	// begins with two dots is not mistaken for an escape.
	if dir == "" || dir == "." || dir == ".." || strings.HasPrefix(dir, "../") || strings.HasPrefix(dir, "/") {
		return goPackageDir{}, false
	}
	return goPackageDir{dir: dir, recursive: recursive}, true
}

// joinRepoRel joins a repo-relative base with a possibly-empty relative
// element, cleaning the result. An absolute element replaces the base, matching
// what `cd /abs` does in a shell; such a path is outside the repo and is
// rejected by the caller.
func joinRepoRel(base, elem string) string {
	if elem == "" {
		return base
	}
	if strings.HasPrefix(elem, "/") {
		return elem
	}
	return path.Clean(path.Join(base, elem))
}
