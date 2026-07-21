// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capabilitycatalog

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// RemoteValidationArtifactDir is the directory, relative to the repository
// root, holding one committed evidence file per remote_validation proof-ID
// cited in the capability matrix. A capability profile's remote_validation
// verification ref resolves to
// "<repoRoot>/<RemoteValidationArtifactDir>/<ref>.md"; a ref with no file
// there is unverifiable and must appear in the burn-down baseline or the gate
// fails (#5407, PR 2 of #5336).
const RemoteValidationArtifactDir = "docs/internal/remote-validation"

// RemoteValidationBaselineFileName is the burn-down baseline file inside the
// specs directory: one remote_validation ref slug per line, known-dangling
// debt exempted from the artifact-existence gate.
const RemoteValidationBaselineFileName = "remote-validation-baseline.txt"

// RemoteValidationFrozenFileName is the immutable frozen-set file inside the
// specs directory: the audited-at-introduction set of remote_validation slugs.
// The gate requires the burn-down baseline to be a subset of this set, so a
// NEW unbacked claim cannot be baselined even at constant entry count (the
// atomic-swap defense, #5407). It is never written by -update: a slug leaves
// only when its row is validated-or-downgraded and removed from BOTH files.
const RemoteValidationFrozenFileName = "remote-validation-frozen.txt"

// remoteValidationRefPattern is the slug shape every remote_validation ref
// (and therefore every baseline entry) must match: lowercase alphanumeric
// segments joined by single hyphens. This is the filename-safety and
// baseline-parsing contract, not a rule about what the proof itself covers.
var remoteValidationRefPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// remoteValidationBaselineHeaderProse is the fixed prose block written at the
// top of a regenerated baseline file by RenderRemoteValidationBaseline. The
// dynamic FROZEN_MAX directive line and the slug legend are appended after it.
const remoteValidationBaselineHeaderProse = `# specs/remote-validation-baseline.txt
#
# FROZEN AUDITED DEBT — remote_validation artifact-existence gate
# (#5407, PR 2 of #5336).
#
# Every slug below is a capability-matrix row whose production profile claims
# {status: supported} and whose SOLE verification evidence is a
# remote_validation ref that resolves to NO committed artifact. The evidence
# directory docs/internal/remote-validation/ did not exist when this gate was
# introduced, so each of these is a top-tier production-support claim resting
# on zero committed proof.
#
# This file FREEZES that audited set so the debt cannot grow. It does NOT cure
# the claims: a slug listed here is tracked, not verified.
#
# MEMBERSHIP IS BOUNDED BY THE FROZEN SET. Every slug below MUST also appear in
# specs/remote-validation-frozen.txt (the immutable audited-at-introduction
# set); the gate enforces baseline ⊆ frozen and fails closed if the frozen file
# is missing or malformed. This defeats the atomic swap the FROZEN_MAX ceiling
# alone cannot catch — burning down one baselined ref while adding a NEW
# unbacked claim keeps the entry count constant, but the new claim is absent
# from the frozen set and is rejected. A new claim can never be baselined; it
# clears the gate only with a committed artifact or a real tier downgrade.
#
# The only honest way to remove a slug is one of:
#   1. Run the production validation and commit the reproducible evidence
#      artifact at docs/internal/remote-validation/<slug>.md, or
#   2. Downgrade the row's claimed tier in the capability matrix so it no
#      longer asserts production:supported on unverified evidence.
# Editing this prose, or any other text, is never an exit — only a committed
# artifact or a real tier downgrade clears a slug.
#
# The systemic per-row validate-or-downgrade of every slug below is tracked in
# issue #5552, which blocks epic #5344 closure.
#
# GROWTH IS FORBIDDEN. The FROZEN_MAX directive below is a ratcheting
# high-water mark: the gate fails when the entry count EXCEEDS it, so a new
# unverified production:supported row cannot be smuggled in by appending its
# ref and running -update. Burning down a slug (committing its artifact or
# downgrading its row) and running -update lowers FROZEN_MAX to the new,
# smaller count; -update never raises it. Raising the ceiling requires an
# explicit, separately-reviewed one-line edit that a reviewer can catch.
#
# Two capability/profile rows citing the same ref collapse to one line here
# (the artifact question is per-ref, not per-row).
#
# Regenerate with:
#   cd go && go run ./cmd/capability-inventory -mode remote-validation -update
#
`

// remoteValidationCeilingPattern matches a well-formed FROZEN_MAX directive
// line and captures its non-negative integer value.
var remoteValidationCeilingPattern = regexp.MustCompile(`^#\s*FROZEN_MAX:\s*([0-9]+)\s*$`)

// remoteValidationCeilingIntent matches any line that intends to be a
// FROZEN_MAX directive (case-insensitive), so a malformed one fails closed
// instead of being silently treated as a plain comment.
var remoteValidationCeilingIntent = regexp.MustCompile(`(?i)^#\s*FROZEN_MAX\b`)

// RemoteValidationBaseline is the parsed burn-down baseline: the frozen set of
// dangling remote_validation slugs plus the ratcheting high-water-mark Ceiling
// (the committed FROZEN_MAX directive). The gate forbids the entry set from
// growing past Ceiling; see RemoteValidationBaselineCeilingExceeded.
type RemoteValidationBaseline struct {
	// Entries is the frozen set of dangling remote_validation ref slugs.
	Entries map[string]struct{}
	// Ceiling is the committed FROZEN_MAX high-water mark: the maximum number
	// of entries the baseline may hold. It ratchets down on burn-down and is
	// never raised by regeneration.
	Ceiling int
}

// RemoteValidationBaselineCeilingExceeded reports whether the frozen entry set
// has GROWN past its FROZEN_MAX ceiling. A true result means a new dangling
// ref was added without a compensating burn-down or an explicit ceiling
// raise, which the gate forbids.
func RemoteValidationBaselineCeilingExceeded(b RemoteValidationBaseline) bool {
	return len(b.Entries) > b.Ceiling
}

// RemoteValidationFinding is one remote_validation proof-ID cited in the
// matrix that has no committed artifact and no burn-down baseline entry.
type RemoteValidationFinding struct {
	// Ref is the dangling remote_validation proof-ID.
	Ref string
	// Subjects lists the sorted "<capability>/<profile>" rows citing Ref.
	Subjects []string
}

// CheckRemoteValidationArtifacts verifies every remote_validation ref cited in
// matrix resolves to a committed artifact at
// "<repoRoot>/docs/internal/remote-validation/<ref>.md", or is present in
// baseline (known burn-down debt, see RemoteValidationBaselineFileName).
// Findings are deduplicated by ref (a ref reused by multiple capability/
// profile rows produces one finding) and sorted by ref.
func CheckRemoteValidationArtifacts(matrix Matrix, repoRoot string, baseline map[string]struct{}) []RemoteValidationFinding {
	subjectsByRef := map[string][]string{}
	for _, capability := range matrix.Capabilities {
		for profileID, profile := range capability.Profiles {
			for _, verification := range profile.Verification {
				if verification.Kind != "remote_validation" {
					continue
				}
				subject := capability.Capability + "/" + profileID
				subjectsByRef[verification.Ref] = append(subjectsByRef[verification.Ref], subject)
			}
		}
	}

	var findings []RemoteValidationFinding
	for ref, subjects := range subjectsByRef {
		// A ref that is not a valid slug is a hard finding: it can never be
		// turned into a safe filesystem path, so it is neither artifact-backed
		// nor eligible burn-down debt. Treating it as a finding (rather than
		// skipping the os.Stat) blocks a path-traversal ref from resolving to
		// an unrelated file and from being excused by a baseline entry.
		if remoteValidationRefValid(ref) {
			if remoteValidationArtifactExists(repoRoot, ref) {
				continue
			}
			if _, baselined := baseline[ref]; baselined {
				continue
			}
		}
		sorted := append([]string(nil), subjects...)
		sort.Strings(sorted)
		findings = append(findings, RemoteValidationFinding{Ref: ref, Subjects: sorted})
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].Ref < findings[j].Ref })
	return findings
}

// remoteValidationRefValid reports whether ref matches the remote_validation
// slug contract (lowercase alphanumeric segments joined by single hyphens).
// The slug shape carries no path separators or "." segments, so a valid ref can
// never escape RemoteValidationArtifactDir. This is the filename-safety gate:
// every ref MUST pass it before it is joined into a filesystem path.
func remoteValidationRefValid(ref string) bool {
	return remoteValidationRefPattern.MatchString(ref)
}

// remoteValidationArtifactExists reports whether a committed, non-directory
// artifact exists for ref under repoRoot. It refuses to probe any ref that is
// not a valid slug (FIX 1, #5407): a path-traversal payload such as
// "../../etc/passwd" would otherwise resolve to an arbitrary file and be
// falsely reported as a committed artifact. As defense in depth, the resolved
// path is re-verified to stay under RemoteValidationArtifactDir after cleaning,
// so even a future regex weakening cannot let os.Stat escape the directory.
func remoteValidationArtifactExists(repoRoot, ref string) bool {
	if !remoteValidationRefValid(ref) {
		return false
	}
	dir := filepath.Clean(filepath.Join(repoRoot, RemoteValidationArtifactDir))
	path := filepath.Clean(filepath.Join(dir, ref+".md"))
	if path != dir && !strings.HasPrefix(path, dir+string(filepath.Separator)) {
		return false
	}
	info, err := os.Stat(path) // #nosec G304 -- ref is slug-validated (remoteValidationRefValid) and the resolved path is confirmed under RemoteValidationArtifactDir before this stat, so it cannot escape the evidence directory
	return err == nil && !info.IsDir()
}

// LoadRemoteValidationBaseline reads the burn-down baseline: one slug per
// line, blank lines and '#'-prefixed comments ignored, plus exactly one
// FROZEN_MAX ceiling directive. A missing file is an empty baseline with a
// zero ceiling (no debt and no growth allowance), not an error — an empty
// baseline still rejects any dangling ref, so deleting the file cannot smuggle
// a claim in. For a file that exists, the ceiling is mandatory: an absent,
// duplicated, or malformed FROZEN_MAX directive fails closed, as does any
// entry line that is not a bare, valid remote_validation ref slug.
func LoadRemoteValidationBaseline(path string) (RemoteValidationBaseline, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- caller supplies the fixed repo-relative baseline path
	if err != nil {
		if os.IsNotExist(err) {
			return RemoteValidationBaseline{Entries: map[string]struct{}{}, Ceiling: 0}, nil
		}
		return RemoteValidationBaseline{}, fmt.Errorf("read remote-validation baseline %s: %w", path, err)
	}
	entries := map[string]struct{}{}
	ceiling := -1 // sentinel: FROZEN_MAX not yet seen
	for i, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			if !remoteValidationCeilingIntent.MatchString(trimmed) {
				continue // ordinary comment
			}
			value, ok := parseRemoteValidationCeilingLine(trimmed)
			if !ok {
				return RemoteValidationBaseline{}, fmt.Errorf("remote-validation baseline %s:%d: malformed FROZEN_MAX directive %q (expected '# FROZEN_MAX: <non-negative-int>')", path, i+1, trimmed)
			}
			if ceiling >= 0 {
				return RemoteValidationBaseline{}, fmt.Errorf("remote-validation baseline %s:%d: duplicate FROZEN_MAX directive", path, i+1)
			}
			ceiling = value
			continue
		}
		if !remoteValidationRefPattern.MatchString(trimmed) {
			return RemoteValidationBaseline{}, fmt.Errorf("remote-validation baseline %s:%d: malformed entry %q (expected a bare lowercase-hyphen slug)", path, i+1, trimmed)
		}
		entries[trimmed] = struct{}{}
	}
	if ceiling < 0 {
		return RemoteValidationBaseline{}, fmt.Errorf("remote-validation baseline %s: missing required FROZEN_MAX directive (expected a line like '# FROZEN_MAX: %d')", path, len(entries))
	}
	return RemoteValidationBaseline{Entries: entries, Ceiling: ceiling}, nil
}

// LoadRemoteValidationFrozenSet reads the immutable frozen set: one
// remote_validation slug per line, blank lines and '#'-prefixed comments
// ignored. It fails closed — a missing file, a read error, or any line that is
// not a bare, valid slug returns an error — because the frozen set is the
// membership authority the baseline is checked against (#5407). A gate that
// cannot load the frozen set MUST NOT pass: without it, the atomic-swap defense
// is silently absent. An empty-but-present file (only comments) yields an empty
// set, which correctly rejects every baseline entry.
func LoadRemoteValidationFrozenSet(path string) (map[string]struct{}, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- caller supplies the fixed repo-relative frozen-set path
	if err != nil {
		return nil, fmt.Errorf("read remote-validation frozen set %s: %w", path, err)
	}
	frozen := map[string]struct{}{}
	for i, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !remoteValidationRefValid(trimmed) {
			return nil, fmt.Errorf("remote-validation frozen set %s:%d: malformed entry %q (expected a bare lowercase-hyphen slug)", path, i+1, trimmed)
		}
		frozen[trimmed] = struct{}{}
	}
	return frozen, nil
}

// RemoteValidationBaselineNotFrozen returns the sorted baseline entries that are
// NOT present in the frozen set, i.e. the witnesses that baseline ⊄ frozen. A
// non-empty result means a ref was added to the burn-down baseline that was not
// in the audited-at-introduction frozen set — a NEW unbacked claim smuggled in.
// This is the atomic-swap defense the FROZEN_MAX ceiling alone cannot provide:
// burning down one baselined ref while adding a new one keeps the count
// constant, but the new ref is absent from the frozen set. Burn-down (removing
// a ref from the baseline once its artifact lands) never triggers this, because
// a smaller baseline is still a subset of the frozen set.
func RemoteValidationBaselineNotFrozen(baseline RemoteValidationBaseline, frozen map[string]struct{}) []string {
	var offenders []string
	for entry := range baseline.Entries {
		if _, ok := frozen[entry]; !ok {
			offenders = append(offenders, entry)
		}
	}
	sort.Strings(offenders)
	return offenders
}

// parseRemoteValidationCeilingLine parses a FROZEN_MAX directive line, already
// known to state ceiling intent, into its non-negative integer value. It
// returns ok=false for any value that is not a well-formed non-negative int
// (including integer overflow), so the caller fails closed.
func parseRemoteValidationCeilingLine(trimmed string) (int, bool) {
	m := remoteValidationCeilingPattern.FindStringSubmatch(trimmed)
	if m == nil {
		return 0, false
	}
	value, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, false
	}
	return value, true
}

// ReadRemoteValidationCeiling reads only the FROZEN_MAX ceiling from a baseline
// file, best-effort, for the -update regeneration path. It returns
// (ceiling, true) when a well-formed directive is present and (0, false) when
// the file is missing or carries no valid directive (for example the pre-gate
// baseline that predates FROZEN_MAX). Regeneration uses this to ratchet the
// ceiling DOWN without ever raising it; a not-found result means "no prior
// bound", so the regenerated ceiling is the current dangling count.
func ReadRemoteValidationCeiling(path string) (int, bool) {
	raw, err := os.ReadFile(path) // #nosec G304 -- caller supplies the fixed repo-relative baseline path
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if !remoteValidationCeilingIntent.MatchString(trimmed) {
			continue
		}
		if value, ok := parseRemoteValidationCeilingLine(trimmed); ok {
			return value, true
		}
	}
	return 0, false
}

// RenderRemoteValidationBaseline computes the current dangling remote_validation
// refs (cited in matrix, no committed artifact under repoRoot) and renders them
// as a baseline file body, header and FROZEN_MAX directive included, sorted for
// determinism.
//
// The rendered ceiling ratchets DOWN and is never raised: it is the current
// dangling count, clamped to priorCeiling when priorFound is true and the prior
// ceiling is smaller. When the prior ceiling is smaller than the current count
// (a new dangling ref was added since the last freeze), the rendered baseline
// deliberately holds more entries than its ceiling so the check-mode gate fails
// until the offender commits an artifact or explicitly raises FROZEN_MAX.
func RenderRemoteValidationBaseline(matrix Matrix, repoRoot string, priorCeiling int, priorFound bool) string {
	dangling := map[string]struct{}{}
	for _, capability := range matrix.Capabilities {
		for _, profile := range capability.Profiles {
			for _, verification := range profile.Verification {
				if verification.Kind != "remote_validation" {
					continue
				}
				// An invalid ref is never written into the baseline: it is not a
				// legal slug, so it cannot be turned into a path (FIX 1, #5407)
				// and could never load back through LoadRemoteValidationBaseline.
				// It stays out of the burn-down set and surfaces as a hard
				// finding in check mode instead.
				if !remoteValidationRefValid(verification.Ref) {
					continue
				}
				if remoteValidationArtifactExists(repoRoot, verification.Ref) {
					continue
				}
				dangling[verification.Ref] = struct{}{}
			}
		}
	}
	refs := make([]string, 0, len(dangling))
	for ref := range dangling {
		refs = append(refs, ref)
	}
	sort.Strings(refs)

	ceiling := len(refs)
	if priorFound && priorCeiling < ceiling {
		ceiling = priorCeiling
	}

	var b strings.Builder
	b.WriteString(remoteValidationBaselineHeaderProse)
	fmt.Fprintf(&b, "# FROZEN_MAX: %d\n", ceiling)
	b.WriteString("#\n# <remote-validation-ref-slug>\n")
	for _, ref := range refs {
		b.WriteString(ref)
		b.WriteString("\n")
	}
	return b.String()
}
