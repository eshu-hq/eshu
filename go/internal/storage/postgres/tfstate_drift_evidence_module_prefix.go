// Module-aware drift joining helpers for PostgresDriftEvidenceLoader.
//
// This file walks `terraform_modules` parser facts within the active commit
// anchor and builds a callee-directory → module-prefix map the loader can
// consult while it decodes `terraform_resources` rows. The map keys are
// callee-directory paths exactly as they appear in `terraform_resources.path`
// values (the parser's per-file `path` string). The map values are the
// canonical Terraform-state address prefix for that callee — either one
// prefix string (single caller) or a slice of prefix strings (1→N projection
// for one callee referenced by multiple `module {}` blocks).
//
// NOTE: this file uses `path` (forward-slash semantics), NOT `path/filepath`
// (OS-specific separators). `terraform_modules.path` and
// `terraform_resources.path` are Postgres-stored strings the parser
// normalized to forward slashes — not live filesystem paths. Using
// `filepath.Clean` here would compile and pass tests on macOS/Linux while
// silently mis-splitting paths on Windows builds; the regression is
// invisible to `go vet` and golangci-lint defaults. The
// TestBuildModulePrefixMapForwardSlashSemanticsRegression test locks the
// forward-slash contract in.
//
// Refs:
//   - ADR docs/docs/adrs/2026-05-11-module-aware-drift-joining.md
//   - Issue #169
//   - State-side address shape:
//     go/internal/collector/terraformstate/identity.go:26-42
//   - Parser emission site:
//     go/internal/parser/hcl/parser.go:188-208
package postgres

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"
)

// maxModulePrefixDepth bounds how deep buildModulePrefixMap will follow a
// module-call chain when concatenating prefix segments. Hard-coded to ten
// per the ADR's "more than any Terraform repo Eshu has seen in dogfood
// corpora" justification — the bound exists to make cycles cheap to break,
// not as a real ceiling.
//
// Do NOT promote this to an env-configurable knob (ESHU_DRIFT_*) without
// dogfood evidence that real repos cluster near it. v1 ships hard-coded
// because exposing the knob invites untracked operator tuning before we
// have measurement showing the knob matters.
const maxModulePrefixDepth = 10

// Closed-enum reasons that classify an unresolvable module call. The strings
// must stay byte-identical to the values the
// MetricDimensionDriftUnresolvedModuleReason dimension carries on
// eshu_dp_drift_unresolved_module_calls_total — operators filter dashboards
// on these literals.
const (
	unresolvedReasonExternalRegistry = "external_registry"
	unresolvedReasonExternalGit      = "external_git"
	unresolvedReasonExternalArchive  = "external_archive"
	unresolvedReasonCrossRepoLocal   = "cross_repo_local"
	unresolvedReasonCycleDetected    = "cycle_detected"
	unresolvedReasonDepthExceeded    = "depth_exceeded"
	unresolvedReasonModuleRenamed    = "module_renamed"
)

// unresolvedRecorder reports unresolved module calls so the loader can drive
// telemetry (counter increment + structured log). One method keeps the
// interface small enough for unit tests to substitute a stub recorder; the
// production adapter is a thin shim over *telemetry.Instruments.
type unresolvedRecorder interface {
	record(ctx context.Context, reason string)
}

// nopUnresolvedRecorder discards every record call. The loader uses it when
// Instruments is nil (early bootstrap tests, fixtures without telemetry).
type nopUnresolvedRecorder struct{}

func (nopUnresolvedRecorder) record(context.Context, string) {}

// modulePrefixMap maps callee-directory paths (forward-slash strings) to one
// or more module-address prefixes. A directory has multiple prefixes when
// two or more `module {}` blocks call the same callee — the 1→N projection
// case (ADR row at line 308). The loader emits one ResourceRow per prefix
// for each parser entry whose path lies under the directory.
//
// The value slice never contains an empty string and is deterministically
// ordered (sorted ascending) so test fixtures and dashboards see stable
// output across runs.
type modulePrefixMap map[string][]string

// moduleCallEntry is the decoded shape of one parser-emitted
// `terraform_modules` row. The parser's emission site
// (go/internal/parser/hcl/parser.go:192-208) populates `name`, `source`,
// and `path` on every row; the other attributes are ignored here.
type moduleCallEntry struct {
	// name is the `module "<name>" {}` label. Becomes the per-segment value
	// in the module-prefix string ("module.<name>").
	name string
	// source is the literal `source = "..."` attribute value. Classified by
	// classifyModuleSource to decide whether the call resolves to a local
	// callee directory or falls back to an unresolved reason.
	source string
	// path is the parser's file path for the .tf file that declares this
	// module block. The call-site directory is path.Dir(path); the callee
	// directory is path.Clean(path.Join(path.Dir(path), source)) when the
	// source resolves locally.
	path string
}

// classifyModuleSource maps a raw `source` attribute string to either a
// cleaned local-directory path (under the repo snapshot root) or a closed-
// enum "unresolved" reason. The two return values are mutually exclusive:
// if reason is "" the callee directory was resolved; otherwise the caller
// must report the reason and skip the call.
//
// Classification rules in priority order:
//
//  1. Empty / whitespace-only source — external_archive (catch-all for
//     unparseable parser emissions).
//  2. Starts with "git::", "git@", or one of the well-known forge https
//     prefixes ("https://github.com/", "https://gitlab.com/",
//     "https://bitbucket.org/") — external_git. The check is HasPrefix, not
//     substring: a bare "github.com/..." source with no scheme falls through
//     to the registry/local branches below, matching Terraform's own source
//     resolution rules.
//  3. Starts with "http://" or with any other "https://" scheme not matched
//     above — external_archive (treat HTTP archives and unparseable schemes
//     as the catch-all).
//  4. Starts with "s3::", "gcs::", "mercurial::" — external_archive
//     (per the ADR's Q3 resolution: do NOT split sub-types).
//  5. Starts with "./" or "../" — local relative path; resolve.
//  6. Otherwise: if the source contains a slash and matches the Terraform
//     Registry shorthand "namespace/name/provider" (three segments, no
//     leading `.`), classify as external_registry. The ADR covers
//     "terraform-aws-modules/vpc/aws", "hashicorp/consul/aws".
//  7. Otherwise: treat as a local relative path with no `./` prefix
//     (Terraform accepts "modules/vpc" as a relative source). Resolve.
//
// `callSiteDir` is path.Dir of the calling .tf file. The resolved callee
// directory is `path.Clean(path.Join(callSiteDir, source))`. If the cleaned
// result starts with ".." (escapes the repo snapshot root), the call is
// classified as cross_repo_local.
func classifyModuleSource(callSiteDir, source string) (callee string, reason string) {
	trimmed := strings.TrimSpace(source)
	if trimmed == "" {
		return "", unresolvedReasonExternalArchive
	}
	// Git URL forms.
	if strings.HasPrefix(trimmed, "git::") || strings.HasPrefix(trimmed, "git@") {
		return "", unresolvedReasonExternalGit
	}
	// Bitbucket / GitHub / GitLab https URLs are git refs in Terraform's
	// source resolution rules even without the explicit git:: scheme.
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "https://github.com/") ||
		strings.HasPrefix(lower, "https://gitlab.com/") ||
		strings.HasPrefix(lower, "https://bitbucket.org/") {
		return "", unresolvedReasonExternalGit
	}
	// Plain HTTP archives, plus any other non-git scheme prefix.
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return "", unresolvedReasonExternalArchive
	}
	// Explicit cloud / VCS scheme prefixes per Terraform docs.
	for _, prefix := range []string{"s3::", "gcs::", "mercurial::", "hg::"} {
		if strings.HasPrefix(lower, prefix) {
			return "", unresolvedReasonExternalArchive
		}
	}
	// Local relative path forms — ./ or ../ — always resolve.
	if strings.HasPrefix(trimmed, "./") || strings.HasPrefix(trimmed, "../") ||
		trimmed == "." || trimmed == ".." {
		return resolveLocalCallee(callSiteDir, trimmed)
	}
	// Registry shorthand: "namespace/name/provider" — three slash-separated
	// non-empty segments, none starting with `.`, none containing schemes.
	// Real local paths with three components ("modules/vpc/inner") will
	// also match the segment count; disambiguate by checking that the
	// FIRST segment does not look like a directory the calling repo
	// would contain (it has no path separator characters in the segment
	// itself — already true — AND it contains no other path metadata).
	//
	// The ADR is explicit that this discriminator is heuristic: a repo
	// whose top-level dir is literally "terraform-aws-modules" will
	// false-positive as registry. v1 accepts that as a measurable
	// fallback; operators see the counter and the fixture matrix in the
	// test corpus exercises both classifications.
	if segments := strings.Split(trimmed, "/"); len(segments) == 3 &&
		!strings.HasPrefix(segments[0], ".") &&
		segments[0] != "" && segments[1] != "" && segments[2] != "" &&
		!strings.ContainsAny(trimmed, "\\:") {
		return "", unresolvedReasonExternalRegistry
	}
	// Otherwise treat as a local relative source ("modules/vpc").
	return resolveLocalCallee(callSiteDir, trimmed)
}

// resolveLocalCallee joins the call-site directory with a local source and
// reports the cleaned callee directory. Escapes ("..") past the repo root
// classify as cross_repo_local.
func resolveLocalCallee(callSiteDir, source string) (callee string, reason string) {
	joined := path.Join(callSiteDir, source)
	cleaned := path.Clean(joined)
	// path.Clean preserves a leading ".." when the join walked above the
	// notional root. Postgres-stored file paths are repo-relative, so any
	// leading ".." means the call escapes the repo snapshot.
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", unresolvedReasonCrossRepoLocal
	}
	// path.Clean returns "." for the repo root or for a callSiteDir of "."
	// joined with an empty-ish source. A module call whose callee resolves
	// to the repo root is meaningless but harmless; surface it as an empty
	// callee so the prefix walker treats it like a no-op rather than
	// inheriting every resource in the repo.
	if cleaned == "." {
		return "", unresolvedReasonExternalArchive
	}
	return cleaned, ""
}

// buildModulePrefixMap reads `terraform_modules` facts for one commit anchor
// and returns the callee-directory → module-prefix map. Every unresolved
// call increments the counter via `recorder.record` and is otherwise
// skipped. Cycles and depth-exceeded chains are detected during prefix
// concatenation, not during the initial decode.
//
// The returned map is keyed by cleaned callee-directory paths (forward
// slashes). One key can carry multiple prefixes when several `module {}`
// blocks reference the same callee — the 1→N projection case. The loader
// emits one ResourceRow per prefix for each parser entry whose `path` lies
// under the directory; that fan-out is deliberately NOT folded into
// configRowFromParserEntry so the per-entry row builder stays strictly
// 1:1 (architectural contract per binding constraint D).
func (l PostgresDriftEvidenceLoader) buildModulePrefixMap(
	ctx context.Context,
	scopeID string,
	generationID string,
	recorder unresolvedRecorder,
) (modulePrefixMap, error) {
	if recorder == nil {
		recorder = nopUnresolvedRecorder{}
	}
	rows, err := l.DB.QueryContext(ctx, listModuleCallsForCommitQuery, scopeID, generationID)
	if err != nil {
		return nil, fmt.Errorf("list config terraform_modules: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var entries []moduleCallEntry
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("scan config terraform_modules: %w", err)
		}
		decoded, err := decodeJSONArray(raw, "terraform_modules")
		if err != nil {
			return nil, err
		}
		for _, entry := range decoded {
			name := strings.TrimSpace(coerceJSONString(entry["name"]))
			if name == "" {
				continue
			}
			entries = append(entries, moduleCallEntry{
				name:   name,
				source: coerceJSONString(entry["source"]),
				path:   strings.TrimSpace(coerceJSONString(entry["path"])),
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate config terraform_modules: %w", err)
	}
	if len(entries) == 0 {
		return modulePrefixMap{}, nil
	}

	// Two passes. Pass 1: classify every call into (callerDir → call) edges
	// keyed by the call-site directory. Pass 2: for every root-level call
	// (call-site directory has no parent caller in the edge map) walk down
	// the chain depth-first, concatenating "module.<name>" segments,
	// tracking visited callees per expansion to break cycles, and bounding
	// the chain at maxModulePrefixDepth.

	// callerToCalls indexes module calls by their call-site directory
	// (path.Dir(entry.path)). One directory can host multiple calls.
	callerToCalls := map[string][]moduleCallEdge{}
	// callees collects every callee directory recorded as the *target* of
	// a module call. The DFS only starts from call-sites that are NOT
	// themselves callees of another call — that is, the OUTERMOST callers.
	// Without this, a callee that itself hosts nested module {} blocks
	// would be visited twice: once as a root and once as the chained
	// descendant of its caller, producing both a long ("module.outer.
	// module.inner") and a short ("module.inner") prefix string for the
	// same callee directory. The short prefix is wrong because the
	// resource's runtime address is governed by the outermost caller.
	callees := map[string]struct{}{}
	for _, entry := range entries {
		callSiteDir := path.Dir(entry.path)
		if callSiteDir == "" {
			callSiteDir = "."
		}
		callee, reason := classifyModuleSource(callSiteDir, entry.source)
		if reason != "" {
			recorder.record(ctx, reason)
			continue
		}
		callerToCalls[callSiteDir] = append(callerToCalls[callSiteDir], moduleCallEdge{
			name:   entry.name,
			callee: callee,
		})
		callees[callee] = struct{}{}
	}
	if len(callerToCalls) == 0 {
		return modulePrefixMap{}, nil
	}

	// Identify the true root call-sites: directories that host module {}
	// blocks but are NOT themselves callees of any other module call.
	// Walking the graph from non-roots in addition would emit short,
	// misleading prefixes for the same callee that has a longer prefix
	// through the outer caller.
	rootCallers := make([]string, 0, len(callerToCalls))
	for callerDir := range callerToCalls {
		if _, isCallee := callees[callerDir]; !isCallee {
			rootCallers = append(rootCallers, callerDir)
		}
	}
	// In a pure-cycle graph (every call-site is also a callee) there are
	// no roots. Fall back to walking every node so cycle detection can
	// fire and operators see the unresolved counter. This is rare in real
	// repos and the regression test
	// TestBuildModulePrefixMapDetectsCycleAndBreaks exercises it.
	if len(rootCallers) == 0 {
		for callerDir := range callerToCalls {
			rootCallers = append(rootCallers, callerDir)
		}
	}

	out := modulePrefixMap{}
	for _, callerDir := range rootCallers {
		for _, call := range callerToCalls[callerDir] {
			walkModulePrefixChain(
				ctx,
				call,
				callerToCalls,
				"module."+call.name,
				1,
				map[string]struct{}{call.callee: {}},
				out,
				recorder,
			)
		}
	}

	// Sort each prefix slice so test fixtures and dashboards see stable
	// ordering. Map iteration above is non-deterministic.
	for key := range out {
		sort.Strings(out[key])
	}
	return out, nil
}

// moduleCallEdge captures one resolved local-callee edge in the module call
// graph. Stored per call-site directory in `callerToCalls`.
type moduleCallEdge struct {
	// name is the `module "<name>" {}` label; becomes the segment value.
	name string
	// callee is the cleaned callee directory under the repo snapshot.
	callee string
}

// walkModulePrefixChain performs the DFS over the module call graph. At
// each callee directory it (a) records the running prefix for the callee
// and (b) follows every child call hosted under that directory. Cycle
// detection uses a per-expansion `visited` set so two independent walks
// through the same callee do not falsely collide.
//
// The function operates by side effect — it appends to `out` and records
// telemetry through `recorder` — to keep the recursive signature short
// and avoid per-step allocation of return slices.
func walkModulePrefixChain(
	ctx context.Context,
	call moduleCallEdge,
	callerToCalls map[string][]moduleCallEdge,
	prefix string,
	depth int,
	visited map[string]struct{},
	out modulePrefixMap,
	recorder unresolvedRecorder,
) {
	if depth > maxModulePrefixDepth {
		recorder.record(ctx, unresolvedReasonDepthExceeded)
		return
	}
	out[call.callee] = appendUniquePrefix(out[call.callee], prefix)
	for _, child := range callerToCalls[call.callee] {
		if _, seen := visited[child.callee]; seen {
			recorder.record(ctx, unresolvedReasonCycleDetected)
			continue
		}
		nextVisited := cloneVisited(visited)
		nextVisited[child.callee] = struct{}{}
		walkModulePrefixChain(
			ctx,
			child,
			callerToCalls,
			prefix+".module."+child.name,
			depth+1,
			nextVisited,
			out,
			recorder,
		)
	}
}

// appendUniquePrefix returns `slice` with `prefix` appended only when the
// prefix is not already present. The 1→N projection case relies on this:
// two callers of the same callee produce two distinct prefixes that must
// both land; same caller observed twice (e.g. through two starting paths
// in the walk) must not double-emit.
func appendUniquePrefix(slice []string, prefix string) []string {
	for _, existing := range slice {
		if existing == prefix {
			return slice
		}
	}
	return append(slice, prefix)
}

// cloneVisited returns a shallow copy of the visited set so a sibling DFS
// path does not see callees the current expansion added.
func cloneVisited(in map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{}, len(in)+1)
	for k := range in {
		out[k] = struct{}{}
	}
	return out
}

// modulePrefixForPath returns the module-prefix strings that apply to a
// parser entry's `path` field. The longest-matching callee directory wins
// — a file at `modules/platform/vpc/main.tf` consults
// `modules/platform/vpc` before `modules/platform` — but if the longest
// match has multiple prefix entries (1→N case) the function returns all
// of them. Empty slice means "no prefix; emit the root-module address."
func (m modulePrefixMap) modulePrefixForPath(filePath string) []string {
	if len(m) == 0 {
		return nil
	}
	cleaned := path.Clean(filePath)
	dir := path.Dir(cleaned)
	if dir == "" {
		dir = "."
	}
	// Walk up the directory chain until either a match is found or the
	// chain reaches the snapshot root. The longest-prefix-wins property
	// is a natural consequence of starting at the file's own directory
	// and walking up.
	for {
		if prefixes, ok := m[dir]; ok && len(prefixes) > 0 {
			return prefixes
		}
		if dir == "." || dir == "/" || dir == "" {
			return nil
		}
		parent := path.Dir(dir)
		if parent == dir {
			return nil
		}
		dir = parent
	}
}
