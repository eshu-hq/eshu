// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"sort"
	"strings"
	"time"
)

const (
	defaultCosignCommand = "cosign"
	defaultCosignTimeout = 2 * time.Minute
)

// CosignProvenanceVerifier verifies component artifacts through the cosign CLI.
type CosignProvenanceVerifier struct {
	Command string
	Timeout time.Duration
}

// VerifyProvenance verifies every manifest artifact signature and attestation.
func (v CosignProvenanceVerifier) VerifyProvenance(
	ctx context.Context,
	manifest Manifest,
	requirement ProvenanceRequirement,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := manifest.Validate(); err != nil {
		return WrapError(ErrorCodeInvalidManifest, err.Error(), err)
	}
	requirementPolicy := ProvenancePolicy(requirement)
	normalizedRequirement, err := requirementPolicy.requirement()
	if err != nil {
		return err
	}
	for i, artifact := range manifest.Spec.Artifacts {
		if err := v.verifySignature(ctx, manifest, artifact, normalizedRequirement, i); err != nil {
			return err
		}
		if err := v.verifyAttestation(ctx, artifact, normalizedRequirement, i); err != nil {
			return err
		}
	}
	return nil
}

func (v CosignProvenanceVerifier) verifySignature(
	ctx context.Context,
	manifest Manifest,
	artifact Artifact,
	requirement ProvenanceRequirement,
	index int,
) error {
	args := []string{
		"verify",
		"--certificate-identity", requirement.CertificateIdentity,
		"--certificate-oidc-issuer", requirement.OIDCIssuer,
		"--check-claims",
		"--output", "json",
	}
	for _, annotation := range cosignManifestAnnotations(manifest) {
		args = append(args, "-a", annotation)
	}
	args = append(args, artifact.Image)
	if err := v.run(ctx, args); err != nil {
		return cosignVerificationError(ErrorCodeProvenanceInvalid, "signature", index, artifact.Image, err)
	}
	return nil
}

func (v CosignProvenanceVerifier) verifyAttestation(
	ctx context.Context,
	artifact Artifact,
	requirement ProvenanceRequirement,
	index int,
) error {
	args := []string{
		"verify-attestation",
		"--certificate-identity", requirement.CertificateIdentity,
		"--certificate-oidc-issuer", requirement.OIDCIssuer,
		"--check-claims",
		"--type", requirement.PredicateType,
		"--output", "json",
		artifact.Image,
	}
	if err := v.run(ctx, args); err != nil {
		return cosignVerificationError(ErrorCodeUnsupportedProvenance, "attestation", index, artifact.Image, err)
	}
	return nil
}

func (v CosignProvenanceVerifier) run(ctx context.Context, args []string) error {
	runCtx, cancel := context.WithTimeout(ctx, v.timeout())
	defer cancel()
	cmd := exec.CommandContext(runCtx, v.command(), args...) // #nosec G204 -- runs cosign with program-constructed args (fixed flags + digest-pinned image ref from manifest); command defaults to "cosign"
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	err := cmd.Run()
	if err != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return context.DeadlineExceeded
		}
		if errors.Is(runCtx.Err(), context.Canceled) {
			return context.Canceled
		}
		return err
	}
	return nil
}

func (v CosignProvenanceVerifier) command() string {
	command := strings.TrimSpace(v.Command)
	if command == "" {
		return defaultCosignCommand
	}
	return command
}

func (v CosignProvenanceVerifier) timeout() time.Duration {
	if v.Timeout <= 0 {
		return defaultCosignTimeout
	}
	return v.Timeout
}

func cosignManifestAnnotations(manifest Manifest) []string {
	return []string{
		"eshu.component.id=" + manifest.Metadata.ID,
		"eshu.component.publisher=" + manifest.Metadata.Publisher,
		"eshu.component.version=" + manifest.Metadata.Version,
		"eshu.component.compatible-core=" + manifest.Spec.CompatibleCore,
		"eshu.component.sdk-protocol=" + manifest.Spec.Runtime.SDKProtocol,
		"eshu.component.adapter=" + manifest.Spec.Runtime.Adapter,
		"eshu.component.collector-kinds=" + sortedJoin(manifest.Spec.CollectorKinds),
		"eshu.component.fact-kinds=" + sortedFactKinds(manifest.Spec.EmittedFacts),
		"eshu.component.fact-schema-versions=" + sortedFactDetails(manifest.Spec.EmittedFacts, factSchemaVersions),
		"eshu.component.fact-payload-schema-refs=" + sortedFactDetails(manifest.Spec.EmittedFacts, factPayloadSchemaRef),
		"eshu.component.fact-source-confidence=" + sortedFactDetails(manifest.Spec.EmittedFacts, factSourceConfidence),
		"eshu.component.reducer-phases=" + sortedJoin(manifest.Spec.ConsumerContracts.Reducer.Phases),
		"eshu.component.metrics-prefix=" + manifest.Spec.Telemetry.MetricsPrefix,
	}
}

func sortedFactKinds(facts []FactFamily) string {
	values := make([]string, 0, len(facts))
	for _, fact := range facts {
		values = append(values, fact.Kind)
	}
	return sortedJoin(values)
}

func sortedFactDetails(facts []FactFamily, valuesFor func(FactFamily) []string) string {
	values := make([]string, 0, len(facts))
	for _, fact := range facts {
		values = append(values, fact.Kind+":"+strings.Join(sortedStrings(valuesFor(fact)), "|"))
	}
	return sortedJoin(values)
}

func factSchemaVersions(fact FactFamily) []string {
	return fact.SchemaVersions
}

func factPayloadSchemaRef(fact FactFamily) []string {
	return []string{fact.PayloadSchemaRef}
}

func factSourceConfidence(fact FactFamily) []string {
	return fact.SourceConfidence
}

func sortedJoin(values []string) string {
	return strings.Join(sortedStrings(values), ",")
}

func sortedStrings(values []string) []string {
	copied := append([]string(nil), values...)
	sort.Strings(copied)
	return copied
}

func cosignVerificationError(code ErrorCode, phase string, index int, image string, err error) error {
	return Errorf(
		code,
		"cosign %s verification failed for artifact %d (%s): %s",
		phase,
		index,
		artifactDigest(image),
		sanitizedCosignFailure(err),
	)
}

func sanitizedCosignFailure(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "command timed out"
	case errors.Is(err, context.Canceled):
		return "command was canceled"
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return "command exited non-zero"
	}
	var pathErr *exec.Error
	if errors.As(err, &pathErr) {
		return "command is unavailable"
	}
	return "command failed"
}

func artifactDigest(image string) string {
	_, digest, ok := strings.Cut(image, "@")
	if !ok || strings.TrimSpace(digest) == "" {
		return "digest unavailable"
	}
	return digest
}
