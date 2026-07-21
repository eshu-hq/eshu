// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capabilitycatalog

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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

// remoteValidationRefPattern is the slug shape every remote_validation ref
// (and therefore every baseline entry) must match: lowercase alphanumeric
// segments joined by single hyphens. This is the filename-safety and
// baseline-parsing contract, not a rule about what the proof itself covers.
var remoteValidationRefPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// remoteValidationBaselineHeader is the fixed header block written at the top
// of a regenerated baseline file by RenderRemoteValidationBaseline.
const remoteValidationBaselineHeader = `# specs/remote-validation-baseline.txt
#
# Burn-down baseline for the remote_validation artifact-existence gate
# (#5407, PR 2 of #5336). Every remote_validation proof-ID cited in
# specs/capability-matrix*.yaml must resolve to a committed artifact at
# docs/internal/remote-validation/<ref>.md, or be listed here as known debt.
# A dangling ref NOT listed here fails the gate; a listed ref that is still
# dangling passes (tracked, not fixed). Once a committed artifact exists for
# a ref, remove it from this file on the next -update run — shrinking this
# file is the only way it changes; a ref is never re-added once its artifact
# lands. Two capability/profile rows citing the same ref collapse to one line
# here (the artifact question is per-ref, not per-row).
#
# Regenerate with:
#   cd go && go run ./cmd/capability-inventory -mode remote-validation -update
#
# <remote-validation-ref-slug>
`

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
		if remoteValidationArtifactExists(repoRoot, ref) {
			continue
		}
		if _, baselined := baseline[ref]; baselined {
			continue
		}
		sorted := append([]string(nil), subjects...)
		sort.Strings(sorted)
		findings = append(findings, RemoteValidationFinding{Ref: ref, Subjects: sorted})
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].Ref < findings[j].Ref })
	return findings
}

// remoteValidationArtifactExists reports whether a committed, non-directory
// artifact exists for ref under repoRoot.
func remoteValidationArtifactExists(repoRoot, ref string) bool {
	path := filepath.Join(repoRoot, RemoteValidationArtifactDir, ref+".md")
	info, err := os.Stat(path) // #nosec G304 -- path is program-constructed from the caller-supplied repoRoot joined with a matrix-declared ref, mirroring readMatrixFile's own accepted pattern
	return err == nil && !info.IsDir()
}

// LoadRemoteValidationBaseline reads the burn-down baseline: one slug per
// line, blank lines and '#'-prefixed comments ignored. A missing file is an
// empty baseline (no debt tracked yet), not an error. Any other line that is
// not a bare, valid remote_validation ref slug fails closed — a malformed
// baseline is a registry bug, never silently skipped.
func LoadRemoteValidationBaseline(path string) (map[string]struct{}, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- caller supplies the fixed repo-relative baseline path
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]struct{}{}, nil
		}
		return nil, fmt.Errorf("read remote-validation baseline %s: %w", path, err)
	}
	baseline := map[string]struct{}{}
	for i, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !remoteValidationRefPattern.MatchString(trimmed) {
			return nil, fmt.Errorf("remote-validation baseline %s:%d: malformed entry %q (expected a bare lowercase-hyphen slug)", path, i+1, trimmed)
		}
		baseline[trimmed] = struct{}{}
	}
	return baseline, nil
}

// RenderRemoteValidationBaseline computes the current dangling remote_validation
// refs (cited in matrix, no committed artifact under repoRoot) and renders
// them as a baseline file body, header included, sorted for determinism.
func RenderRemoteValidationBaseline(matrix Matrix, repoRoot string) string {
	dangling := map[string]struct{}{}
	for _, capability := range matrix.Capabilities {
		for _, profile := range capability.Profiles {
			for _, verification := range profile.Verification {
				if verification.Kind != "remote_validation" {
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

	var b strings.Builder
	b.WriteString(remoteValidationBaselineHeader)
	for _, ref := range refs {
		b.WriteString(ref)
		b.WriteString("\n")
	}
	return b.String()
}
