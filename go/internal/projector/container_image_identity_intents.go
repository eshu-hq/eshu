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
	FactKindFileObserved,
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
	case FactKindFileObserved:
		// A Dockerfile's FROM base images live on the repository's `file` fact
		// (parsed_file_data.dockerfile_stages), never on a content_entity, and
		// the reducer only extracts them inside a container_image_identity
		// intent. When a Dockerfile is the only identity-relevant fact in a
		// generation -- a repository that adds or edits its Dockerfile with no
		// new image evidence -- nothing else here triggers the domain, so the
		// base-image lineage (#5460) would never project and a changed or
		// deleted base would leave the prior DERIVED_FROM edge stale.
		//
		// Narrow by design: every repository generation carries `file` facts, so
		// only a Dockerfile may trigger, never an arbitrary source file.
		return dockerfileIdentityTriggerFile(envelope)
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

// dockerfileIdentityTriggerFile reports whether a `file` fact is a Dockerfile
// whose base-image evidence the container_image_identity domain must re-derive.
//
// Two recognizers, deliberately: parsed base-image stages are the precise signal
// for an added or edited Dockerfile, but a REMOVED Dockerfile arrives as a
// tombstone that can carry no parsed_file_data at all. Falling back to the
// declared language and file name keeps the removal path triggering, which is
// what lets the reducer's retract-first pass clear a DERIVED_FROM edge whose
// Dockerfile is gone (#5460) instead of leaving it stale forever.
func dockerfileIdentityTriggerFile(envelope facts.Envelope) bool {
	if dockerfileStagesPresent(envelope.Payload) {
		return true
	}
	language, _ := payloadString(envelope.Payload, "language")
	if strings.EqualFold(strings.TrimSpace(language), "dockerfile") {
		return true
	}
	rawName, _ := payloadString(envelope.Payload, "name")
	name := strings.TrimSpace(rawName)
	if name == "" {
		relativePath, _ := payloadString(envelope.Payload, "relative_path")
		name = pathBaseName(strings.TrimSpace(relativePath))
	}
	return strings.EqualFold(name, "Dockerfile") || strings.HasPrefix(strings.ToLower(name), "dockerfile.")
}

// dockerfileStagesPresent reports whether a file fact's parsed_file_data carries
// a non-empty dockerfile_stages bucket. Both the in-process []map[string]any and
// the JSON-round-tripped []any shape reach this projector.
func dockerfileStagesPresent(payload map[string]any) bool {
	fileData, ok := payload["parsed_file_data"].(map[string]any)
	if !ok {
		return false
	}
	switch stages := fileData["dockerfile_stages"].(type) {
	case []map[string]any:
		return len(stages) > 0
	case []any:
		return len(stages) > 0
	default:
		return false
	}
}

// pathBaseName returns the final path segment, without importing path/filepath
// for a single separator split on an always-slash-delimited repository path.
func pathBaseName(value string) string {
	if idx := strings.LastIndex(value, "/"); idx >= 0 {
		return value[idx+1:]
	}
	return value
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
