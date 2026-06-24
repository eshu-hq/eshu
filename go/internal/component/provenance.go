// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"context"
	"strings"
)

const (
	// DefaultProvenancePredicateType is the supported SLSA provenance
	// attestation type for strict component trust.
	DefaultProvenancePredicateType = "slsaprovenance1"
)

// ProvenancePolicy configures strict component provenance verification.
type ProvenancePolicy struct {
	CertificateIdentity string
	OIDCIssuer          string
	PredicateType       string
}

// ProvenanceRequirement is the normalized verifier input for one policy check.
type ProvenanceRequirement struct {
	CertificateIdentity string
	OIDCIssuer          string
	PredicateType       string
}

// ProvenanceVerifier verifies artifact signatures and attestations.
type ProvenanceVerifier interface {
	VerifyProvenance(context.Context, Manifest, ProvenanceRequirement) error
}

// ConfigureProvenanceFromEnv attaches strict provenance settings from env.
func ConfigureProvenanceFromEnv(policy Policy, getenv func(string) string) Policy {
	if getenv == nil {
		return policy
	}
	policy.Provenance = ProvenancePolicy{
		CertificateIdentity: strings.TrimSpace(getenv("ESHU_COMPONENT_PROVENANCE_CERTIFICATE_IDENTITY")),
		OIDCIssuer:          strings.TrimSpace(getenv("ESHU_COMPONENT_PROVENANCE_OIDC_ISSUER")),
		PredicateType:       strings.TrimSpace(getenv("ESHU_COMPONENT_PROVENANCE_PREDICATE_TYPE")),
	}
	if strings.TrimSpace(policy.Mode) == TrustModeStrict {
		policy.ProvenanceVerifier = CosignProvenanceVerifier{
			Command: strings.TrimSpace(getenv("ESHU_COMPONENT_COSIGN_BINARY")),
		}
	}
	return policy
}

func (p ProvenancePolicy) requirement() (ProvenanceRequirement, error) {
	identity := strings.TrimSpace(p.CertificateIdentity)
	if identity == "" {
		return ProvenanceRequirement{}, NewError(
			ErrorCodeProvenanceRequired,
			"strict provenance verification requires a certificate identity",
		)
	}
	issuer := strings.TrimSpace(p.OIDCIssuer)
	if issuer == "" {
		return ProvenanceRequirement{}, NewError(
			ErrorCodeProvenanceRequired,
			"strict provenance verification requires an OIDC issuer",
		)
	}
	predicateType := strings.TrimSpace(p.PredicateType)
	if predicateType == "" {
		predicateType = DefaultProvenancePredicateType
	}
	if predicateType != DefaultProvenancePredicateType {
		return ProvenanceRequirement{}, Errorf(
			ErrorCodeUnsupportedProvenance,
			"unsupported provenance predicate type %q; supported type is %q",
			predicateType,
			DefaultProvenancePredicateType,
		)
	}
	return ProvenanceRequirement{
		CertificateIdentity: identity,
		OIDCIssuer:          issuer,
		PredicateType:       predicateType,
	}, nil
}

func (p ProvenancePolicy) isZero() bool {
	return strings.TrimSpace(p.CertificateIdentity) == "" &&
		strings.TrimSpace(p.OIDCIssuer) == "" &&
		strings.TrimSpace(p.PredicateType) == ""
}
