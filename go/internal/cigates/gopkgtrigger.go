// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"fmt"
	"path"
	"strings"
)

// goPackageSubcommands are the `go` subcommands whose `./pkg` arguments name
// code that the gate compiles and runs. `go list` is deliberately absent: it
// reports on packages without building or running their tests, so a gate that
// only lists a package is not made false-green by a change inside it.
var goPackageSubcommands = map[string]struct{}{
	"build": {},
	"run":   {},
	"test":  {},
	"vet":   {},
}

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
//   - `cd` is honoured wherever it appears, not only at the start, because
//     ci-gate-registry reaches its own package through a mid-command subshell
//     (`... && (cd go && go test ./internal/cigates -count=1)`). The directory
//     is tracked forward through the whole command; shell scoping of a subshell
//     `cd` is NOT modelled, which can only ever attribute a package to a deeper
//     directory than the shell would use. That direction is safe: it demands a
//     more specific trigger than strictly necessary, so it cannot manufacture a
//     false green.
//   - A package argument that resolves outside the repository is skipped, not
//     reported. It names no in-repo file whose edit could go unselected.
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
	inGoPackageArgs := false
	seen := make(map[goPackageDir]struct{}, len(fields))
	var out []goPackageDir

	for i := 0; i < len(fields); i++ {
		w := strings.TrimLeft(fields[i], "(")

		if _, isSep := shellSeparators[w]; isSep {
			inGoPackageArgs = false
			continue
		}

		if w == "cd" && i+1 < len(fields) {
			cwd = joinRepoRel(cwd, strings.Trim(fields[i+1], `"'`))
			i++
			inGoPackageArgs = false
			continue
		}

		if w == "go" && i+1 < len(fields) {
			_, ok := goPackageSubcommands[strings.TrimRight(fields[i+1], `"'`)]
			inGoPackageArgs = ok
			i++
			continue
		}

		if !inGoPackageArgs || !strings.HasPrefix(w, "./") {
			continue
		}

		pkg, ok := resolvePackageArg(cwd, strings.TrimRight(w, `"'`))
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
	if dir == "" || dir == "." || strings.HasPrefix(dir, "..") {
		// "" and "." are the repository root: every path is inside it, so the
		// self-trigger property is vacuous and there is nothing to assert.
		// A ".."-prefixed dir escaped the repo.
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
