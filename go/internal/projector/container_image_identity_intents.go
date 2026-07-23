// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// containerImageIdentityCandidateFactKinds are the fact kinds
// containerImageIdentityTriggerFact ever returns true for. Kept as an
// explicit list (rather than discovered per generation like the open-registry
// probes) because it names concrete, closed fact-kind constants — the same
// set containerImageIdentityTriggerFact's switch already enumerates.
var containerImageIdentityCandidateFactKinds = []string{
	facts.OCIImageManifestFactKind,
	facts.OCIImageIndexFactKind,
	facts.OCIImageTagObservationFactKind,
	facts.OCIImageReferrerFactKind,
	facts.AWSImageReferenceFactKind,
	facts.AzureImageReferenceFactKind,
	facts.GCPImageReferenceFactKind,
	facts.AWSRelationshipFactKind,
	facts.CICDArtifactFactKind,
	"content_entity",
	facts.AttestationSLSAProvenanceFactKind,
	facts.AttestationSignatureVerificationFactKind,
}

func buildContainerImageIdentityReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	index *reducerIntentFactIndex,
) (ReducerIntent, bool) {
	envelope, ok := index.firstAcrossKinds(containerImageIdentityTriggerFact, containerImageIdentityCandidateFactKinds...)
	if !ok {
		return ReducerIntent{}, false
	}
	return ReducerIntent{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: generation.GenerationID,
		Domain:       reducer.DomainContainerImageIdentity,
		EntityKey:    "container_image_identity:" + scopeValue.ScopeID,
		Reason:       "container image identity evidence observed",
		FactID:       envelope.FactID,
		SourceSystem: containerImageIdentitySourceSystem(envelope),
	}, true
}

func containerImageIdentityTriggerFact(envelope facts.Envelope) bool {
	switch envelope.FactKind {
	case facts.OCIImageManifestFactKind,
		facts.OCIImageIndexFactKind,
		facts.OCIImageTagObservationFactKind,
		facts.OCIImageReferrerFactKind:
		return true
	case facts.AWSImageReferenceFactKind:
		return true
	case facts.AzureImageReferenceFactKind:
		return true
	case facts.GCPImageReferenceFactKind:
		return true
	case facts.AWSRelationshipFactKind:
		relationship, err := decodeAWSRelationship(envelope)
		if err != nil {
			return false
		}
		return codegraphDerefString(relationship.TargetType) == "container_image"
	case facts.CICDArtifactFactKind:
		// A container-image artifact carries the digest that joins its run's
		// commit to an OCI manifest. Triggering the identity intent for the CI
		// scope is what lets the reducer co-load the scope-local ci.run/ci.artifact
		// with the cross-scope active OCI manifest, so the #5423 commit-revision
		// threading is reachable in production (the OCI collector writes its
		// manifest in a different scope). Non-image artifacts (coverage reports,
		// SBOM bundles) carry no image reference, so they must not trigger.
		artifactType, _ := payloadString(envelope.Payload, "artifact_type")
		return strings.TrimSpace(artifactType) == "container_image"
	case "content_entity":
		return len(containerImageRefsFromEntityMetadata(envelope.Payload)) > 0
	case facts.AttestationSLSAProvenanceFactKind:
		// A signed SLSA provenance predicate carries the digest-to-commit
		// anchor the reducer's container_image_identity domain joins by
		// statement_id (#5456 PR #5707 P1-b). It lives in the SBOM-attestation
		// collector's own scope, a different scope than the OCI manifest it
		// must eventually override a weaker tier for, so this fact must
		// trigger its OWN refresh — otherwise SLSA evidence landing with no
		// other new identity evidence in the same generation would never
		// cause the reducer to re-derive the affected image's decision.
		return true
	case facts.AttestationSignatureVerificationFactKind:
		// The #5456 PR #5707 P1-a verification gate requires a PASSED
		// signature_verification fact before the SLSA tier applies. A
		// verification result can land in a later generation than its
		// statement/provenance (an async re-verification pass), so it must
		// independently trigger a refresh too, or a decision could never
		// flip from unverified to verified after the fact.
		return true
	default:
		return false
	}
}

func containerImageIdentitySourceSystem(envelope facts.Envelope) string {
	if value := strings.TrimSpace(envelope.SourceRef.SourceSystem); value != "" {
		return value
	}
	return strings.TrimSpace(envelope.CollectorKind)
}

func containerImageRefsFromEntityMetadata(payload map[string]any) []string {
	for _, key := range []string{"entity_metadata", "metadata"} {
		metadata, ok := payload[key].(map[string]any)
		if !ok {
			continue
		}
		refs := cleanStringValues(metadata["container_images"])
		if len(refs) > 0 {
			return refs
		}
	}
	return nil
}

func cleanStringValues(value any) []string {
	switch typed := value.(type) {
	case []string:
		return cleanStrings(typed)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, strings.TrimSpace(fmt.Sprint(item)))
		}
		return cleanStrings(out)
	case string:
		return cleanStrings([]string{typed})
	default:
		return nil
	}
}

func cleanStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}
