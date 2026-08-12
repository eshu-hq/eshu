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

// goPackageGateCount is how many local gates in specs/ci-gates.v1.yaml run at
// least one Go package from their own local.command or local.test_command —
// the set this check covers and checkScriptTriggerCoverage structurally cannot
// see.
//
// It lives in a constant guarded by TestGoPackageGateCount rather than in prose
// because this package has already been burned twice by a hand-written count:
// checkVerifyScriptWorkflowMatch's doc comment hard-coded 29 gates and the
// registry grew past it silently, and the first version of THIS file's comment
// said 19 by miscounting gates with no scripts/ token (which includes the npm
// ones) as Go gates. A reviewer caught the second one. Keep the figure in
// exactly one place that a test can check.
//
// #6055 dropped this from 18 to 17: authz-scoped-route-tests' local.command
// now calls scripts/go-test-run-guard.sh (a go_test_run_guard wrapper that
// fails loudly on a zero-matched `-run` pin) instead of invoking `go test
// ./internal/query ...` as a literal token this extractor recognizes. That
// gate's Go-package trigger coverage is not weakened by the drop —
// "go/internal/query/**" is already an explicit, correct trigger on it — but
// checkGoPackageTriggerCoverage's derived cross-check no longer independently
// re-confirms that fact for this one gate the way it does for the other 17.
const goPackageGateCount = 17

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
// edit runs. A sizeable minority of the registry's local gates are implemented
// as a Go package (`go run ./cmd/x`, `go test ./internal/y`) rather than a
// scripts/ file, and checkScriptTriggerCoverage cannot see any of them — it
// only derives "scripts/"-prefixed tokens. Editing the program that IS the gate
// went unchecked for exactly the gates whose logic is hardest to review by eye
// (#5873). The exact figure lives in goPackageGateCount below rather than in
// this sentence, for the reason scriptworkflow.go's doc comment records: a
// hard-coded count in prose here went stale silently once already.
//
// The package set is derived, not declared, so a gate added later inherits the
// property instead of having to remember it.
//
// Narrowings, all deliberate:
//
//   - Only the subcommands in goPackageSubcommands count. `go list -deps
//     ./...` in the sdk gates reports on packages without running them, so a
//     change inside a merely-listed package cannot alter the gate's verdict.
//   - A package token is attributed to the most recent `go <subcommand>` on the
//     same command, and the run ends at the next shell separator. A bare
//     `./pkg` with no preceding go subcommand (none in the registry today) is
//     ignored rather than guessed at. A subcommand that names NO package is not
//     ignored: `go help packages` says an omitted import path means the package
//     in the current directory, and `go help run` accepts a bare `.`, so both
//     forms yield the working directory. Deriving nothing from them would make
//     `cd go/internal/x && go test` a silently unchecked gate.
//   - `cd` and `go -C <dir>` are both honoured, wherever they appear rather
//     than only at the start, because ci-gate-registry reaches its own package
//     through a mid-command subshell (`... && (cd go && go test
//     ./internal/cigates -count=1)`). Subshell scope IS modelled: a `cd` inside
//     `( )` is popped at the closing paren. An earlier version tracked one
//     directory forward through the whole command and claimed that was safe
//     because it could only attribute a package to a deeper directory than the
//     shell would use. That claim was wrong (#5955 review, codex). Deeper is
//     not safer when a broader ancestor trigger exists: in `(cd
//     sdk/go/collector && go test ./...) && (cd sdk/go/factschema && go test
//     ./...)` the second package resolved to
//     sdk/go/collector/sdk/go/factschema, which `sdk/go/collector/**` matches
//     at any depth — a false GREEN over the real sdk/go/factschema.
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
// They are chosen to demand DIRECTORY-WIDE coverage rather than to be matched
// by some particular filename, which an earlier version got wrong: a single
// invented name such as "zz_selftrigger_probe.go" let a narrow trigger like
// `go/internal/x/zz*.go` read as covering the package while excluding every
// ordinary source file in it (#5955 review, codex).
//
// Two shapes are required, and both matter:
//
//   - an ordinary Go source name, the common case;
//   - a NON-Go file, because a package's compiled output depends on more than
//     its .go files. `go/internal/capabilitycatalog/*.go` looks like package
//     coverage but misses data/catalog.generated.json, which is embedded into
//     the package — editing it changes what the gate tests while selecting
//     nothing. Demanding this shape means a package trigger has to be
//     directory-wide (`dir/**`), not extension-narrowed.
//
// A recursive spec adds a nested path, since that is what `./...` compiles.
func (p goPackageDir) probes() []string {
	probes := []string{
		path.Join(p.dir, "a.go"),
		path.Join(p.dir, "embedded_asset.json"),
	}
	if p.recursive {
		probes = append(probes, path.Join(p.dir, "nested", "a.go"))
	}
	return probes
}

// triggerCoversPackage reports whether a single trigger matches every probe the
// package needs. One trigger must cover them all: two triggers that each match a
// different probe do not prove any single glob spans the package.
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
	// subshellCwd is the directory stack for "( ... )". A `cd` inside a subshell
	// does not outlive it, and letting it leak is not the safe direction it
	// looks like: in `(cd sdk/go/collector && go test ./...) && (cd
	// sdk/go/factschema && go test ./...)` the second package would resolve to
	// sdk/go/collector/sdk/go/factschema, which a `sdk/go/collector/**` trigger
	// matches at any depth — so the check would pass while edits under the real
	// sdk/go/factschema went unselected. That is a false GREEN, which is why
	// scope is modelled rather than documented away (#5955 review, codex).
	var subshellCwd []string
	inGoCommand := false
	inGoPackageArgs := false
	sawPackageArg := false
	seen := make(map[goPackageDir]struct{}, len(fields))
	var out []goPackageDir

	add := func(pkg goPackageDir) {
		if _, dup := seen[pkg]; dup {
			return
		}
		seen[pkg] = struct{}{}
		out = append(out, pkg)
	}

	// endGoRun closes the current `go` invocation. A subcommand that named no
	// package operates on the current directory (`go help packages`: an omitted
	// import path means the package in the current directory), so the run still
	// yields one — otherwise `cd go/internal/x && go test` derives nothing and
	// the gate is silently unchecked (#5955 review, codex).
	endGoRun := func() {
		if inGoPackageArgs && !sawPackageArg {
			if pkg, ok := resolvePackageArg(cwd, "."); ok {
				add(pkg)
			}
		}
		inGoCommand = false
		inGoPackageArgs = false
		sawPackageArg = false
	}

	for i := 0; i < len(fields); i++ {
		raw := fields[i]
		opens := len(raw) - len(strings.TrimLeft(raw, "("))
		closes := len(raw) - len(strings.TrimRight(raw, ")"))
		for range opens {
			subshellCwd = append(subshellCwd, cwd)
		}

		w := strings.Trim(raw, argTrimCutset)

		popSubshells := func() {
			for range closes {
				if len(subshellCwd) == 0 {
					continue
				}
				cwd = subshellCwd[len(subshellCwd)-1]
				subshellCwd = subshellCwd[:len(subshellCwd)-1]
				endGoRun()
			}
		}

		if _, isSep := shellSeparators[w]; isSep {
			endGoRun()
			popSubshells()
			continue
		}

		if w == "cd" && i+1 < len(fields) {
			cwd = joinRepoRel(cwd, strings.Trim(fields[i+1], argTrimCutset))
			i++
			endGoRun()
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
			endGoRun()
			inGoCommand = true
			popSubshells()
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

		// "." and "./..." are as much a package argument as "./pkg" — `go help
		// run` accepts a bare "." explicitly, and "./..." carries the "./"
		// prefix already. A BARE "..." is not a relative reference: go treats
		// only paths starting with ".", ".." or "/" as filesystem-relative, so
		// "..." is a standard import path naming a package called "...".
		// Accepting it resolved to the working directory and demanded a
		// trigger for it — loud rather than silent, but wrong (#5955 review).
		// An import-path pattern that is NOT filesystem-relative: a bare "..."
		// or an absolute "/...". It names packages, so the command did not
		// "omit its import path" and the current-directory inference in
		// endGoRun must not fire — but it names them by import path rather
		// than by directory, so there is no repo path to demand a trigger for
		// either. Record that an argument was given and resolve nothing.
		if inGoPackageArgs && (w == "..." || strings.HasPrefix(w, "/")) {
			sawPackageArg = true
			popSubshells()
			continue
		}

		isPackageArg := strings.HasPrefix(w, "./") || w == "."
		if !inGoPackageArgs || !isPackageArg {
			popSubshells()
			continue
		}

		sawPackageArg = true
		if pkg, ok := resolvePackageArg(cwd, w); ok {
			add(pkg)
		}
		popSubshells()
	}
	endGoRun()
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
