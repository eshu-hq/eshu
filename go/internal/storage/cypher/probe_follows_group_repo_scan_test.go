// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// executeProbeReceiverPattern mirrors executeGroupReceiverPattern
// (probe_follows_group_test.go) but for ExecuteProbe, so the repo-wide scan
// below can check Group-implies-Probe by source text alone, without
// constructing anything.
var executeProbeReceiverPattern = regexp.MustCompile(`(?m)^func \((?:[A-Za-z_][A-Za-z0-9_]* )?\*?([A-Za-z_][A-Za-z0-9_]*)\) ExecuteProbe\(`)

// executeGroupProbeAllowlist names every ExecuteGroup receiver outside this
// package that TestExecuteGroupImpliesExecuteProbeAcrossRepo currently
// accepts without a matching ExecuteProbe, and why. An entry here is a
// decision, not a shrug: each reason states whether the type can never
// meaningfully support a probe (a test double with no real backend) or
// simply does not have one yet (a tracked, known gap, not a silent one).
// Removing an entry because its type gained ExecuteProbe is expected and
// welcome; removing one to silence a real regression is not what this map
// is for.
var executeGroupProbeAllowlist = map[string]string{
	"bootstrapNeo4jExecutor": "cmd/bootstrap-index: no ExecuteProbe yet. The #5998 review F1 " +
		"pass found this gap; bootstrap-index's executor chain does not currently reach any " +
		"RetractEdges dispatch that would use it, so it is a known gap recorded here rather " +
		"than a live defect -- no follow-up issue is filed. Adding ExecuteProbe here is out " +
		"of scope for the rationale retract guard this test protects -- remove this entry " +
		"when it lands.",
	"projectorNeo4jExecutor": "cmd/projector: no ExecuteProbe yet, same reasoning as " +
		"bootstrapNeo4jExecutor above -- the projector's executor chain does not " +
		"reach a RetractEdges dispatch either.",
	"ingesterNeo4jExecutor": "cmd/ingester: no ExecuteProbe yet, same reasoning as " +
		"bootstrapNeo4jExecutor above -- the ingester's executor chain does not reach " +
		"a RetractEdges dispatch.",
	"groupCapableIngesterExecutor": "cmd/ingester: test-only stub executor (cmd/ingester/wiring_test_helpers.go), " +
		"referenced solely from cmd/ingester/*_test.go files, never constructed by " +
		"production wiring code. No real backend to probe.",
	"recordingGroupChunkExecutor": "cmd/ingester: test-only stub executor, same file and same reasoning as " +
		"groupCapableIngesterExecutor above.",
}

// receiverTypeSite names one ExecuteGroup receiver type and the package
// directory (relative to the go module root) its file lives in, so error
// messages can point a reader straight at the source.
type receiverTypeSite struct {
	typeName string
	pkgDir   string
}

// scanExecuteGroupAndProbeReceivers walks every non-_test.go file under
// go/cmd and go/internal and returns every ExecuteGroup receiver found, plus
// a set recording which (package directory, type name) pairs also have an
// ExecuteProbe receiver somewhere in that same package. The Probe lookup is
// package-scoped, not file-scoped: a type's two methods do not have to live
// in the same file for this check to see both.
//
// Unlike executeGroupReceiverTypesFromSource (probe_follows_group_test.go,
// this package's in-package scan), this walk does NOT skip files gated behind
// the ifafaultinjection build tag (#6165 review F3). That in-package scan's
// skip exists because it feeds wrapperProbeFollowsGroupCases, a
// construct-and-assert table in an untagged file that cannot reference a
// tag-gated type without the build tag -- a real constraint. This scan has no
// such constraint: it is a pure source-text regex match over raw file bytes,
// never compiled or constructed, so it can and must see tag-gated production
// files too. Copying the in-package skip here previously left
// cmd/reducer/ifa_fault_wiring.go's ifaExecutorRetryArmedExecutor -- a real
// wrapper in the live reducer executor chain under the ifafaultinjection tag
// -- structurally invisible to the one gate meant to catch exactly this bug
// class: a decorator that forwards ExecuteGroup but not ExecuteProbe.
func scanExecuteGroupAndProbeReceivers(t *testing.T) (groupReceivers []receiverTypeSite, hasProbe map[string]bool) {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	// thisFile is go/internal/storage/cypher/probe_follows_group_repo_scan_test.go;
	// the go module root is three directories up.
	goModuleRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))

	hasProbe = make(map[string]bool)
	for _, top := range []string{"cmd", "internal"} {
		root := filepath.Join(goModuleRoot, top)
		walkErr := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			src, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			pkgDir, relErr := filepath.Rel(goModuleRoot, filepath.Dir(path))
			if relErr != nil {
				return relErr
			}
			for _, match := range executeGroupReceiverPattern.FindAllSubmatch(src, -1) {
				groupReceivers = append(groupReceivers, receiverTypeSite{typeName: string(match[1]), pkgDir: pkgDir})
			}
			for _, match := range executeProbeReceiverPattern.FindAllSubmatch(src, -1) {
				hasProbe[pkgDir+"::"+string(match[1])] = true
			}
			return nil
		})
		if walkErr != nil {
			t.Fatalf("filepath.Walk(%s): %v", root, walkErr)
		}
	}
	return groupReceivers, hasProbe
}

// TestExecuteGroupImpliesExecuteProbeAcrossRepo is the repo-wide half of the
// #5998 review F1 fix. TestWrapperProbeFollowsGroupTableCoversEveryExecuteGroupReceiver
// (probe_follows_group_test.go) only scans this package's own directory --
// accurate for the in-package behavioral table, but blind to the seam the F1
// review found unforwarded: go/cmd/reducer/reducer_executor_adapters.go,
// outside this package, where cmd-package types are unexported and cannot be
// constructed from here to run the build-and-assert check
// TestWrapperProbeFollowsGroup uses (that would require importing an
// unexported type from another package, or an import cycle the other way).
//
// This test takes the same invariant -- a decorator that forwards
// ExecuteGroup must also forward ExecuteProbe -- and checks it the one way
// that works across package boundaries without construction: purely by
// source text, walking every non-test .go file under go/cmd and go/internal,
// including files gated behind a build tag such as ifafaultinjection (#6165
// review F3 -- see scanExecuteGroupAndProbeReceivers for why this scan does
// not skip them), and asserting that every type with an ExecuteGroup
// receiver also has an ExecuteProbe receiver SOMEWHERE in the same package,
// unless it is named in executeGroupProbeAllowlist with a reason. It cannot
// prove the forwarding is behaviorally correct (only TestWrapperProbeFollowsGroup's
// construct-and-assert shape can, and it stays scoped to this package for that
// reason); it proves the omission can no longer be silent anywhere in the repo.
func TestExecuteGroupImpliesExecuteProbeAcrossRepo(t *testing.T) {
	t.Parallel()

	groupReceivers, hasProbe := scanExecuteGroupAndProbeReceivers(t)
	if len(groupReceivers) == 0 {
		t.Fatal("scan found zero ExecuteGroup receivers under go/cmd and go/internal -- the scan itself is broken, not proof there is nothing to check")
	}
	seen := make(map[string]bool, len(groupReceivers))
	for _, site := range groupReceivers {
		key := site.pkgDir + "::" + site.typeName
		if seen[key] {
			continue
		}
		seen[key] = true
		if hasProbe[key] {
			continue
		}
		if reason, allowlisted := executeGroupProbeAllowlist[site.typeName]; allowlisted {
			if !strings.Contains(reason, site.pkgDir) {
				t.Errorf("executeGroupProbeAllowlist[%q] does not name its own package directory (%s) in its reason -- reasons must be specific, not bare names", site.typeName, site.pkgDir)
			}
			continue
		}
		t.Errorf("type %s (%s) implements ExecuteGroup but has no ExecuteProbe anywhere in its package, and is not in executeGroupProbeAllowlist -- add ExecuteProbe, or add an allowlist entry with a specific reason", site.typeName, site.pkgDir)
	}
}
