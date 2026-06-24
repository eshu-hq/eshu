// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relguard

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// arnPrefix is the canonical AWS ARN scheme prefix every scanner uses to test
// ARN shape. The runtime layer reuses it so its ARN-shape check matches the
// isARN helpers spread across the scanner packages.
const arnPrefix = "arn:"

// Violation is one runtime graph-join contract breach found in an emitted
// relationship. The fields name the offending edge so a scanner test failure
// points at the exact relationship rather than just a count.
type Violation struct {
	// RelationshipType is the edge's relationship_type, for identification.
	RelationshipType string
	// TargetType is the offending target_type value (may be empty).
	TargetType string
	// TargetResourceID is the edge's join key, for identification.
	TargetResourceID string
	// Reason explains why the edge breaches the graph-join contract.
	Reason string
}

// Error renders a Violation as a single diagnostic line.
func (v Violation) Error() string {
	return fmt.Sprintf(
		"relationship %q -> target_type %q (target_resource_id %q): %s",
		v.RelationshipType, v.TargetType, v.TargetResourceID, v.Reason,
	)
}

// TB is the minimal testing surface AssertObservations needs. *testing.T and
// *testing.B both satisfy it, so the helper works from any scanner test without
// importing testing into this support package's public signature beyond this
// interface.
type TB interface {
	Helper()
	Errorf(format string, args ...any)
}

// Check applies the runtime graph-join contract to a batch of emitted
// relationship observations and returns every violation it finds. It is the
// data-dependent counterpart to the static layer: it sees the concrete
// target_type a helper or field read produced, which the AST walk cannot
// resolve. Per observation it asserts:
//
//   - target_type is non-empty;
//   - target_type is a known resource family (declared ResourceType constant or
//     a documented KnownTargetTypeAllowlist entry);
//   - when target_arn is set it is ARN-shaped, and the join key
//     (target_resource_id, or target_arn when the id is blank) is then also
//     ARN-shaped, so an ARN-typed target is not silently keyed by a bare name.
//
// known is the target-type set; pass KnownTargetTypeSet to use the live set.
func Check(known map[string]struct{}, observations ...awscloud.RelationshipObservation) []Violation {
	var violations []Violation
	for _, obs := range observations {
		violations = append(violations, checkOne(known, obs)...)
	}
	return violations
}

// checkOne returns the violations for a single relationship observation.
func checkOne(known map[string]struct{}, obs awscloud.RelationshipObservation) []Violation {
	targetType := strings.TrimSpace(obs.TargetType)
	relationshipType := strings.TrimSpace(obs.RelationshipType)
	targetID := strings.TrimSpace(obs.TargetResourceID)
	targetARN := strings.TrimSpace(obs.TargetARN)

	var violations []Violation
	add := func(reason string) {
		violations = append(violations, Violation{
			RelationshipType: relationshipType,
			TargetType:       targetType,
			TargetResourceID: targetID,
			Reason:           reason,
		})
	}

	if targetType == "" {
		add("empty target_type: a downstream join cannot type the target node")
		return violations
	}
	if _, ok := known[targetType]; !ok {
		add("unknown target_type: not a declared awscloud.ResourceType constant " +
			"and not in relguard.KnownTargetTypeAllowlist; the edge would dangle")
	}
	if targetARN != "" && !isARNShaped(targetARN) {
		add("target_arn is set but is not ARN-shaped (expected an arn: prefix)")
	}
	// Join-mode consistency: when the scanner populated target_arn the target is
	// ARN-keyed, so the resolved join key must also be ARN-shaped. The envelope
	// falls back to target_arn when target_resource_id is blank, so a blank id is
	// not a defect here; a non-blank, non-ARN id alongside a populated ARN is.
	if targetARN != "" && targetID != "" && !isARNShaped(targetID) {
		add("target_arn is set (ARN-keyed target) but target_resource_id is a bare " +
			"value, not an ARN; the edge will not join the ARN-keyed target resource")
	}
	return violations
}

// AssertObservations is the one-line helper a scanner test calls to enforce the
// runtime graph-join contract on the relationships it emits. It fails the test
// with one Errorf per violation and is a no-op when every edge is well formed.
// It resolves the live target-type set itself so callers pass only their
// observations.
func AssertObservations(t TB, observations ...awscloud.RelationshipObservation) {
	t.Helper()
	known, err := KnownTargetTypeSet()
	if err != nil {
		t.Errorf("relguard: resolve known target types: %v", err)
		return
	}
	for _, v := range Check(known, observations...) {
		t.Errorf("relguard: %s", v.Error())
	}
}

var (
	knownOnce sync.Once
	knownSet  map[string]struct{}
	knownErr  error
)

// KnownTargetTypeSet returns the live union of declared ResourceType constant
// values and the documented allowlist, resolving the awscloud source directory
// from this package's own location so any caller gets the same set. The result
// is computed once and cached, so feeding many observations through the runtime
// layer in a test does not re-walk the source tree per call.
func KnownTargetTypeSet() (map[string]struct{}, error) {
	knownOnce.Do(func() {
		knownSet, knownErr = KnownTargetTypes(awscloudSourceDir())
	})
	return knownSet, knownErr
}

// awscloudSourceDir resolves go/internal/collector/awscloud from this file's
// location: runtime.go -> relguard -> internal -> awscloud.
func awscloudSourceDir() string {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	return filepath.Join(filepath.Dir(currentFile), "..", "..")
}

// isARNShaped reports whether value has the canonical AWS ARN prefix, matching
// the isARN helpers the scanner packages use.
func isARNShaped(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), arnPrefix)
}
