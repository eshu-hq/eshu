// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package containerimageidentity

import (
	"fmt"
	"path"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// containerImageIdentityFileFactKind mirrors root's FactKindFileObserved
// ("file", declared in go/internal/projector/stage_facts.go) exactly. This
// package cannot import root — root imports this package to dispatch, so the
// reverse direction cycles — so the shared literal is duplicated here rather
// than referenced.
const containerImageIdentityFileFactKind = "file"

// candidateFactKinds are the fact kinds triggerFact ever returns true for.
// Kept as an explicit list (rather than discovered per generation like the
// open-registry probes) because it names concrete, closed fact-kind
// constants — the same set triggerFact's switch already enumerates.
var candidateFactKinds = []string{
	facts.OCIImageManifestFactKind,
	facts.OCIImageIndexFactKind,
	facts.OCIImageTagObservationFactKind,
	facts.OCIImageReferrerFactKind,
	facts.AWSImageReferenceFactKind,
	facts.AzureImageReferenceFactKind,
	facts.GCPImageReferenceFactKind,
	facts.AWSRelationshipFactKind,
	facts.CICDArtifactFactKind,
	facts.CICDWorkflowImageEvidenceFactKind,
	"content_entity",
	containerImageIdentityFileFactKind,
	facts.AttestationSLSAProvenanceFactKind,
	facts.AttestationSignatureVerificationFactKind,
}

// BuildContainerImageIdentityReducerIntent enqueues one
// container_image_identity reducer intent per scope generation that observed
// OCI digest/tag/referrer facts, AWS/Azure/GCP image-reference facts, an
// AWS container-image relationship, a CI/CD container-image artifact, static
// CI/CD workflow-image evidence, a Git content-entity image reference, a
// Dockerfile (add/edit or tombstoned removal), a signed SLSA provenance
// statement, or a signature-verification result. The reducer owns the
// cross-source digest-first join; this package only selects the trigger
// fact and its source-system label.
func BuildContainerImageIdentityReducerIntent(
	scopeID string,
	generationID string,
	lookup projectorintent.FactLookup,
) (projectorintent.ReducerIntent, bool) {
	envelope, ok := lookup.FirstAcrossKinds(triggerFact, candidateFactKinds...)
	if !ok {
		return projectorintent.ReducerIntent{}, false
	}
	return projectorintent.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainContainerImageIdentity,
		EntityKey:    "container_image_identity:" + scopeID,
		Reason:       "container image identity evidence observed",
		FactID:       envelope.FactID,
		SourceSystem: projectorintent.SourceSystem(envelope),
	}, true
}

func triggerFact(envelope facts.Envelope) bool {
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
		return awsRelationshipTargetsContainerImage(envelope)
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
	case facts.CICDWorkflowImageEvidenceFactKind:
		// Static workflow evidence is emitted in the Git repository scope. It
		// must trigger container_image_identity there so the existing durable
		// identity-completion chain can reopen ci_cd_run_correlation after the
		// workflow generation becomes active. Deletions arrive as the generic
		// file tombstone handled below.
		return true
	case "content_entity":
		return len(containerImageRefsFromEntityMetadata(envelope.Payload)) > 0
	case containerImageIdentityFileFactKind:
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
		// only a Dockerfile or a deleted direct GitHub Actions workflow may
		// trigger, never an arbitrary source file.
		return dockerfileIdentityTriggerFile(envelope) || deletedWorkflowImageTriggerFile(envelope)
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

// deletedWorkflowImageTriggerFile recognizes the generic `file` tombstone the
// Git collector emits when a GitHub Actions workflow is deleted. No
// ci.workflow_image_evidence tombstone exists on this path, so this trigger is
// what schedules the identity refresh that retracts the workflow's old image
// input. Live workflow files trigger through their dedicated evidence facts.
func deletedWorkflowImageTriggerFile(envelope facts.Envelope) bool {
	if !envelope.IsTombstone {
		return false
	}
	relativePath, _ := payloadString(envelope.Payload, "relative_path")
	normalized := strings.ToLower(strings.ReplaceAll(relativePath, "\\", "/"))
	ext := path.Ext(normalized)
	return path.Dir(normalized) == ".github/workflows" && (ext == ".yml" || ext == ".yaml")
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
