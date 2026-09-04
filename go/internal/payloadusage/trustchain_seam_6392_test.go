// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package payloadusage

import (
	"path/filepath"
	"sort"
	"testing"
)

// fixtureTrustChainAnchorCalls mirrors the decode-call shape of
// go/internal/storage/postgres/secrets_iam_trust_chain_anchor_decode.go: the
// loader decodes directly through factschema.Decode* with no local wrapper,
// then reads join-anchor fields off each decoded value — including the real
// file's switch-on-fact-kind dispatch that reuses the `policy` identifier
// across sibling case clauses for different seams, so the scanner must
// attribute each branch's reads to that branch's own decode call rather than
// the last binding of the shared name.
const fixtureTrustChainAnchorCalls = `package postgres

import "github.com/eshu-hq/eshu/sdk/go/factschema"

func anchorAWS(schemaEnv factschema.Envelope, factKind string) {
	switch factKind {
	case "aws_iam_trust_policy":
		policy, err := factschema.DecodeAWSIAMTrustPolicy(schemaEnv)
		if err != nil {
			return
		}
		_ = policy.RoleARN
		_ = policy.WebIdentitySubjectFingerprints
		// Nested branch that binds nothing itself: its reads must resolve
		// against this case's bindings, never vanish (#6392 review).
		// The second nested case binds again (its own region) and reads an
		// identifier bound only mid-chain: that read must resolve through
		// the chained overlay, never vanish either (N1).
		switch factKind {
		case "aws_iam_trust_policy":
			_ = policy.Effect
		case "aws_iam_permission_boundary":
			boundary, err := factschema.DecodeAWSIAMPermissionBoundary(schemaEnv)
			if err != nil {
				return
			}
			_ = boundary.PrincipalARN
			_ = policy.PolicySource
		}
	case "aws_iam_permission_policy":
		policy, err := factschema.DecodeAWSIAMPermissionPolicy(schemaEnv)
		if err != nil {
			return
		}
		_ = policy.PrincipalARN
	case "aws_iam_policy_attachment":
		attachment, err := factschema.DecodeAWSIAMPolicyAttachment(schemaEnv)
		if err != nil {
			return
		}
		_ = attachment.PrincipalARN
	case "aws_iam_permission_boundary":
		policy, err := factschema.DecodeAWSIAMPermissionBoundary(schemaEnv)
		if err != nil {
			return
		}
		_ = policy.PrincipalARN
	}
}

func anchorGCP(schemaEnv factschema.Envelope, factKind string) {
	switch factKind {
	case "gcp_iam_principal":
		principal, err := factschema.DecodeGCPIAMPrincipal(schemaEnv)
		if err != nil {
			return
		}
		_ = principal.PrincipalFingerprint
	case "gcp_iam_trust_policy":
		policy, err := factschema.DecodeGCPIAMTrustPolicy(schemaEnv)
		if err != nil {
			return
		}
		_ = policy.TargetPrincipalFingerprint
		_ = policy.TargetServiceAccountEmailDigest
		_ = policy.GCPWorkloadIdentitySubjectFingerprint
	case "gcp_iam_permission_policy":
		policy, err := factschema.DecodeGCPIAMPermissionPolicy(schemaEnv)
		if err != nil {
			return
		}
		_ = policy.PrincipalFingerprint
	}
}

func anchorKubernetes(schemaEnv factschema.Envelope) {
	posture, err := factschema.DecodeKubernetesServiceAccountTokenPosture(schemaEnv)
	if err != nil {
		return
	}
	_ = posture.ServiceAccountJoinKey
}
`

// TestScanDecodeUsageSeesTrustChainAnchorDecodes is the regression guard for
// issue #6392: eight qualified SDK decode calls in the loader's trust-chain
// anchor decoder have no seam declaration in any factschema_decode*.go, so
// decodeFuncs never contains their names and every field read off their
// results silently disappears from the manifest. A schema field deleted from
// one of those kinds then stays green — a false-green gate.
//
// Unlike the fixture-seam tests in usage_qualified_call_test.go, the seams
// here come from the real schemadecode tree (ParseDecodeSeamsGlob over the
// checked-in factschema_decode*.go files), so this test fails exactly while
// the production seam set is missing any of the eight and passes once each
// has a wrapper plus schema mapping.
func TestScanDecodeUsageSeesTrustChainAnchorDecodes(t *testing.T) {
	t.Parallel()

	seams, err := ParseDecodeSeamsGlob(filepath.Join(repoRoot(t), "go", "internal", "reducer", "schemadecode", "factschema_decode*.go"))
	if err != nil {
		t.Fatalf("ParseDecodeSeamsGlob() error = %v", err)
	}

	dir := writeFixtureDir(t, map[string]string{
		"trust_chain_anchor_calls.go": fixtureTrustChainAnchorCalls,
	})
	usage, err := ScanDecodeUsage(dir, seams, nil, KnownDecodeQualifiers)
	if err != nil {
		t.Fatalf("ScanDecodeUsage() error = %v", err)
	}

	want := map[string][]string{
		"DecodeAWSIAMTrustPolicy":                    {"Effect", "PolicySource", "RoleARN", "WebIdentitySubjectFingerprints"},
		"DecodeAWSIAMPermissionPolicy":               {"PrincipalARN"},
		"DecodeAWSIAMPolicyAttachment":               {"PrincipalARN"},
		"DecodeAWSIAMPermissionBoundary":             {"PrincipalARN"},
		"DecodeGCPIAMPrincipal":                      {"PrincipalFingerprint"},
		"DecodeGCPIAMTrustPolicy":                    {"GCPWorkloadIdentitySubjectFingerprint", "TargetPrincipalFingerprint", "TargetServiceAccountEmailDigest"},
		"DecodeGCPIAMPermissionPolicy":               {"PrincipalFingerprint"},
		"DecodeKubernetesServiceAccountTokenPosture": {"ServiceAccountJoinKey"},
	}
	names := make([]string, 0, len(want))
	for funcName := range want {
		names = append(names, funcName)
	}
	sort.Strings(names)
	for _, funcName := range names {
		for _, field := range want[funcName] {
			found := false
			for _, e := range usage[funcName] {
				if e.GoFieldName == field {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("field %q read off a factschema.%s call was not attributed; got usage[%q] = %+v — that decode has no seam declaration in any factschema_decode*.go (#6392)", field, funcName, funcName, usage[funcName])
			}
		}
	}
}
