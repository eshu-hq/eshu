// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/ifa/familyodu"
)

// Committed-cassette loaders for the seven direct-materialization family Odùs
// (#6228, under the #6181 umbrella).
//
// A cassette is a recorded collector output the live gates replay instead of
// calling a real cluster or cloud account. Each family keeps two copies of the
// same fixture: the Go-compiled Odù that catalog_seed.go registers, and the
// committed cassette the gates drive. They must not drift, so the lockstep
// tests project the cassette back through loadDirectFamilyOdu and require it to
// equal the compiled Odù exactly.
//
// All seven families share one loader rather than carrying a copy each. The
// shared-projection families each grew their own decoder because each reads a
// different fact shape; these seven read the SAME envelope fields and differ
// only in which Odù name the projection carries, so a copy per family would be
// one place per family for one contract to rot.

const (
	// kubernetesNamespaceEnvironmentFamilyCassettePath,
	// iamInstanceProfileRoleFamilyCassettePath, iamCanAssumeFamilyCassettePath,
	// iamCanPerformFamilyCassettePath,
	// workloadCloudRelationshipFamilyCassettePath,
	// iamEscalationFamilyCassettePath, and ec2UsesProfileFamilyCassettePath
	// are repo-root-relative so the constants stay machine-independent;
	// callers join a repo root onto them through the exported helpers below.
	kubernetesNamespaceEnvironmentFamilyCassettePath = "testdata/cassettes/kubernetesnamespaceenvironment/ifa-kubernetes-namespace-environment-family.json"
	iamInstanceProfileRoleFamilyCassettePath         = "testdata/cassettes/iaminstanceprofilerole/ifa-iam-instance-profile-role-family.json"
	iamCanAssumeFamilyCassettePath                   = "testdata/cassettes/iamcanassume/ifa-iam-can-assume-family.json"
	iamCanPerformFamilyCassettePath                  = "testdata/cassettes/iamcanperform/ifa-iam-can-perform-family.json"
	workloadCloudRelationshipFamilyCassettePath      = "testdata/cassettes/workloadcloudrelationship/ifa-workload-cloud-relationship-family.json"
	iamEscalationFamilyCassettePath                  = "testdata/cassettes/iamescalation/ifa-iam-escalation-family.json"
	ec2UsesProfileFamilyCassettePath                 = "testdata/cassettes/ec2usesprofile/ifa-ec2-uses-profile-family.json"
)

// KubernetesNamespaceEnvironmentFamilyCassetteFullPath joins repoRoot onto the
// committed kubernetes_namespace_environment family cassette path.
func KubernetesNamespaceEnvironmentFamilyCassetteFullPath(repoRoot string) string {
	return filepath.Join(repoRoot, kubernetesNamespaceEnvironmentFamilyCassettePath)
}

// IAMInstanceProfileRoleFamilyCassetteFullPath joins repoRoot onto the
// committed iam_instance_profile_role family cassette path.
func IAMInstanceProfileRoleFamilyCassetteFullPath(repoRoot string) string {
	return filepath.Join(repoRoot, iamInstanceProfileRoleFamilyCassettePath)
}

// directFamilyCassetteFile mirrors the committed cassette's v1 wire shape.
//
// Every field the cassette carries is declared, including the ones the
// projection deliberately drops (fencing_token, observed_at, the scope
// descriptors). The decoder rejects unknown fields, so an undeclared field
// would fail the load rather than being ignored -- and a field declared here
// but not projected is a deliberate exclusion, documented at the projection.
type directFamilyCassetteFile struct {
	Collector     string `json:"collector"`
	SchemaVersion string `json:"schema_version"`
	Scopes        []struct {
		ScopeID       string            `json:"scope_id"`
		SourceSystem  string            `json:"source_system"`
		ScopeKind     string            `json:"scope_kind"`
		CollectorKind string            `json:"collector_kind"`
		PartitionKey  string            `json:"partition_key"`
		Metadata      map[string]string `json:"metadata"`
		GenerationID  string            `json:"generation_id"`
		ObservedAt    string            `json:"observed_at"`
		TriggerKind   string            `json:"trigger_kind"`
		Facts         []struct {
			FactKind         string         `json:"fact_kind"`
			StableFactKey    string         `json:"stable_fact_key"`
			SchemaVersion    string         `json:"schema_version"`
			CollectorKind    string         `json:"collector_kind"`
			FencingToken     int64          `json:"fencing_token"`
			SourceConfidence string         `json:"source_confidence"`
			IsTombstone      bool           `json:"is_tombstone,omitempty"`
			Payload          map[string]any `json:"payload"`
		} `json:"facts"`
	} `json:"scopes"`
}

// LoadKubernetesNamespaceEnvironmentFamilyOdu reads the committed cassette and
// projects it onto the fact envelopes the reducer's extractor consumes.
//
// It is the lockstep loader for that cassette: production registers the
// compiled familyodu.KubernetesNamespaceEnvironmentFamilyOdu in catalogSeed, and
// TestKubernetesNamespaceEnvironmentFamilyCassetteMatchesCompiledOdu compares
// the two. Exported so materializededges' tests can reach it without a second
// copy of the decoder.
func LoadKubernetesNamespaceEnvironmentFamilyOdu(cassettePath string) (familyodu.Odu, error) {
	return loadDirectFamilyOdu(cassettePath, KubernetesNamespaceEnvironmentFamilyOduName)
}

// LoadIAMInstanceProfileRoleFamilyOdu reads the committed cassette and projects
// it onto the fact envelopes the reducer's extractor consumes, the
// iam_instance_profile_role counterpart to
// LoadKubernetesNamespaceEnvironmentFamilyOdu.
func LoadIAMInstanceProfileRoleFamilyOdu(cassettePath string) (familyodu.Odu, error) {
	return loadDirectFamilyOdu(cassettePath, IAMInstanceProfileRoleFamilyOduName)
}

// IAMCanAssumeFamilyCassetteFullPath joins repoRoot onto the committed
// iam_can_assume family cassette path.
func IAMCanAssumeFamilyCassetteFullPath(repoRoot string) string {
	return filepath.Join(repoRoot, iamCanAssumeFamilyCassettePath)
}

// LoadIAMCanAssumeFamilyOdu reads the committed cassette and projects it onto
// the fact envelopes the reducer's extractor consumes, the iam_can_assume
// counterpart to LoadIAMInstanceProfileRoleFamilyOdu.
func LoadIAMCanAssumeFamilyOdu(cassettePath string) (familyodu.Odu, error) {
	return loadDirectFamilyOdu(cassettePath, IAMCanAssumeFamilyOduName)
}

// IAMCanPerformFamilyCassetteFullPath joins repoRoot onto the committed
// iam_can_perform family cassette path.
func IAMCanPerformFamilyCassetteFullPath(repoRoot string) string {
	return filepath.Join(repoRoot, iamCanPerformFamilyCassettePath)
}

// LoadIAMCanPerformFamilyOdu reads the committed cassette and projects it onto
// the fact envelopes the reducer's extractor consumes, the iam_can_perform
// counterpart to LoadIAMCanAssumeFamilyOdu.
func LoadIAMCanPerformFamilyOdu(cassettePath string) (familyodu.Odu, error) {
	return loadDirectFamilyOdu(cassettePath, IAMCanPerformFamilyOduName)
}

// WorkloadCloudRelationshipFamilyCassetteFullPath joins repoRoot onto the
// committed workload_cloud_relationship family cassette path.
func WorkloadCloudRelationshipFamilyCassetteFullPath(repoRoot string) string {
	return filepath.Join(repoRoot, workloadCloudRelationshipFamilyCassettePath)
}

// LoadWorkloadCloudRelationshipFamilyOdu reads the committed cassette and
// projects it onto the fact envelopes the reducer's extractor consumes, the
// workload_cloud_relationship counterpart to LoadIAMCanPerformFamilyOdu.
func LoadWorkloadCloudRelationshipFamilyOdu(cassettePath string) (familyodu.Odu, error) {
	return loadDirectFamilyOdu(cassettePath, WorkloadCloudRelationshipFamilyOduName)
}

// IAMEscalationFamilyCassetteFullPath joins repoRoot onto the committed
// iam_escalation family cassette path.
func IAMEscalationFamilyCassetteFullPath(repoRoot string) string {
	return filepath.Join(repoRoot, iamEscalationFamilyCassettePath)
}

// LoadIAMEscalationFamilyOdu reads the committed cassette and projects it
// onto the fact envelopes the reducer's extractor consumes, the
// iam_escalation counterpart to LoadWorkloadCloudRelationshipFamilyOdu.
func LoadIAMEscalationFamilyOdu(cassettePath string) (familyodu.Odu, error) {
	return loadDirectFamilyOdu(cassettePath, IAMEscalationFamilyOduName)
}

// EC2UsesProfileFamilyCassetteFullPath joins repoRoot onto the committed
// ec2_uses_profile family cassette path.
func EC2UsesProfileFamilyCassetteFullPath(repoRoot string) string {
	return filepath.Join(repoRoot, ec2UsesProfileFamilyCassettePath)
}

// LoadEC2UsesProfileFamilyOdu reads the committed cassette and projects it
// onto the fact envelopes the reducer's extractor consumes, the
// ec2_uses_profile counterpart to LoadIAMEscalationFamilyOdu.
func LoadEC2UsesProfileFamilyOdu(cassettePath string) (familyodu.Odu, error) {
	return loadDirectFamilyOdu(cassettePath, EC2UsesProfileFamilyOduName)
}

// loadDirectFamilyOdu is the shared strict projection behind all seven loaders.
//
// It fails closed on an empty scope or fact list: an Odù carrying no facts
// makes every downstream assertion vacuous, which is the failure mode the whole
// exhaustiveness effort exists to remove.
//
// The decoder disallows unknown fields, so a typo anywhere in the envelope
// (e.g. "fact_knd" for "fact_kind") fails loudly at load time instead of
// silently decoding to a zero value and projecting the WRONG fact set from a
// cassette whose green gate run would then attest to something it never said.
// json.Decoder.Decode reads exactly one JSON value and stops, so the second
// Decode requiring io.EOF closes the trailing-content gap.
func loadDirectFamilyOdu(cassettePath, oduName string) (familyodu.Odu, error) {
	raw, err := os.ReadFile(cassettePath) // #nosec G304 -- checked-in repo fixture under testdata/, not external input
	if err != nil {
		return familyodu.Odu{}, fmt.Errorf("ifa: read %s cassette %s: %w", oduName, cassettePath, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var parsed directFamilyCassetteFile
	if err := decoder.Decode(&parsed); err != nil {
		return familyodu.Odu{}, fmt.Errorf("ifa: parse %s cassette %s: %w", oduName, cassettePath, err)
	}
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return familyodu.Odu{}, fmt.Errorf("ifa: %s cassette %s has trailing content after its JSON object", oduName, cassettePath)
	}
	if len(parsed.Scopes) != 1 {
		return familyodu.Odu{}, fmt.Errorf(
			"ifa: %s cassette %s declares %d scopes, want exactly 1; a multi-scope fixture would make the expected-edge set ambiguous about which scope produced an edge",
			oduName, cassettePath, len(parsed.Scopes),
		)
	}
	scope := parsed.Scopes[0]
	if len(scope.Facts) == 0 {
		return familyodu.Odu{}, fmt.Errorf("ifa: %s cassette %s carries no facts; an empty Odù makes every assertion vacuous", oduName, cassettePath)
	}

	// FencingToken and the scope descriptors are deliberately not projected:
	// the compiled Odù does not carry them, they are replay-transport concerns
	// rather than fixture truth, and projecting them would make the lockstep
	// comparison fail on fields no extractor reads.
	envelopes := make([]facts.Envelope, 0, len(scope.Facts))
	for _, fact := range scope.Facts {
		envelopes = append(envelopes, facts.Envelope{
			ScopeID:          scope.ScopeID,
			GenerationID:     scope.GenerationID,
			FactKind:         fact.FactKind,
			SchemaVersion:    fact.SchemaVersion,
			StableFactKey:    fact.StableFactKey,
			CollectorKind:    fact.CollectorKind,
			SourceConfidence: fact.SourceConfidence,
			IsTombstone:      fact.IsTombstone,
			Payload:          fact.Payload,
		})
	}
	return familyodu.Odu{Name: oduName, Facts: envelopes}, nil
}
