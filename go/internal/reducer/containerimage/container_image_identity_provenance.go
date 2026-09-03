// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package containerimage

import (
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/packagesourcecore"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

func extractOCIConfigProvenanceRefs(envelopes []facts.Envelope) []containerImageRefEvidence {
	repositories := packagesourcecore.ExtractRepositories(envelopes)
	if len(repositories) == 0 {
		return nil
	}
	refs := make([]containerImageRefEvidence, 0)
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.OCIImageManifestFactKind {
			continue
		}
		labels := configLabelMap(envelope.Payload["config_labels"])
		sourceURL, ok := singleOCIConfigSourceLabel(labels)
		if !ok {
			continue
		}
		match, ok := matchOCIConfigSourceRepository(sourceURL, repositories)
		if !ok {
			continue
		}
		digest := payloadcore.PayloadStr(envelope.Payload, "digest")
		repositoryID := payloadcore.OCIRepositoryID(envelope.Payload)
		imageRef := imageRefFromOCIRepositoryID(repositoryID, digest)
		parsed, parsedOK := ParseContainerImageRef(imageRef)
		if !parsedOK {
			continue
		}
		refs = append(refs, containerImageRefEvidence{
			imageRef:            imageRef,
			parsed:              parsed,
			resolvedDigest:      digest,
			sourceRepositoryIDs: []string{match.RepositoryID},
			sourceRevision:      normalizeOCIConfigRevision(labels),
			sourceLabelEvidence: true,
			factIDs:             []string{envelope.FactID},
		})
	}
	sort.SliceStable(refs, func(i, j int) bool {
		return refs[i].imageRef < refs[j].imageRef
	})
	return refs
}

func configLabelMap(raw any) map[string]string {
	switch typed := raw.(type) {
	case map[string]string:
		return typed
	case map[string]any:
		out := make(map[string]string, len(typed))
		for key, value := range typed {
			if trimmed := strings.TrimSpace(key); trimmed != "" {
				out[trimmed] = strings.TrimSpace(strings.Trim(fmt.Sprint(value), `"`))
			}
		}
		return out
	default:
		return nil
	}
}

func singleOCIConfigSourceLabel(labels map[string]string) (string, bool) {
	var sourceValues []string
	for _, key := range []string{
		"org.opencontainers.image.source",
		"org.label-schema.vcs-url",
	} {
		if value := strings.TrimSpace(labels[key]); value != "" && value != "[redacted]" {
			sourceValues = append(sourceValues, value)
		}
	}
	if len(sourceValues) == 0 {
		return "", false
	}
	keys := make(map[string]string, len(sourceValues))
	for _, value := range sourceValues {
		key := packagesourcecore.CanonicalURLKey(value)
		if key == "" {
			return "", false
		}
		keys[key] = value
	}
	if len(keys) != 1 {
		return "", false
	}
	for _, value := range keys {
		return value, true
	}
	return "", false
}

// matchOCIConfigSourceRepository resolves an OCI config source label to the
// one REPOSITORY it names, deduping multiple facts for the same repository
// before applying the exactly-one rule.
//
// A repository legitimately carries several active `repository` facts (more
// than one scope or collector observing it), so counting raw fact matches
// instead of distinct repositories made a second fact for the SAME repository
// look like ambiguity and reject an otherwise-unambiguous label. That kept the
// identity tier this feeds (oci_config_source_label_with_digest) effectively
// dead in any real corpus with duplicate repository facts (#5801) — the tier
// was covered by tests that construct only one repository fact, but unreachable
// in production. Deduping first fixes that: two facts naming the SAME
// repository are one answer twice, not ambiguity. Genuine ambiguity — two
// DISTINCT repository ids claiming the same remote — still resolves to
// neither.
func matchOCIConfigSourceRepository(
	sourceURL string,
	repositories []packagesourcecore.Repository,
) (packagesourcecore.Repository, bool) {
	hint := packagesourcecore.Hint{
		PackageID: "container-image",
		HintKind:  "repository",
		SourceURL: sourceURL,
	}
	active, _ := packagesourcecore.MatchRepositories(hint, repositories)
	distinct := make(map[string]packagesourcecore.Repository, len(active))
	for _, repository := range active {
		distinct[repository.RepositoryID] = repository
	}
	if len(distinct) != 1 {
		return packagesourcecore.Repository{}, false
	}
	for _, repository := range distinct {
		return repository, true
	}
	return packagesourcecore.Repository{}, false
}

// extractOCIConfigBuildProvenanceRefs yields build-provenance-only evidence: for
// each OCI manifest whose config source label resolves to exactly one
// repository (via matchOCIConfigSourceRepository), the fact that repository
// BUILT this digest. It deliberately sets no sourceRepositoryIDs,
// sourceRevision, or sourceLabelEvidence of its own -- extractOCIConfigProvenanceRefs
// above sets those identity-tier fields from the SAME matcher, and merging the
// two refs for the same image reference (mergeContainerImageRef) is what lets a
// label resolve both the identity tier and BuildProvenanceRepositoryIDs
// together (#5460, #5801) -- it only feeds the DERIVED_FROM child gate.
func extractOCIConfigBuildProvenanceRefs(envelopes []facts.Envelope) []containerImageRefEvidence {
	repositories := packagesourcecore.ExtractRepositories(envelopes)
	if len(repositories) == 0 {
		return nil
	}
	refs := make([]containerImageRefEvidence, 0)
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.OCIImageManifestFactKind {
			continue
		}
		labels := configLabelMap(envelope.Payload["config_labels"])
		sourceURL, ok := singleOCIConfigSourceLabel(labels)
		if !ok {
			continue
		}
		match, ok := matchOCIConfigSourceRepository(sourceURL, repositories)
		if !ok {
			continue
		}
		digest := payloadcore.PayloadStr(envelope.Payload, "digest")
		imageRef := imageRefFromOCIRepositoryID(payloadcore.OCIRepositoryID(envelope.Payload), digest)
		parsed, parsedOK := ParseContainerImageRef(imageRef)
		if !parsedOK {
			continue
		}
		refs = append(refs, containerImageRefEvidence{
			imageRef:                     imageRef,
			parsed:                       parsed,
			buildProvenanceRepositoryIDs: []string{match.RepositoryID},
		})
	}
	sort.SliceStable(refs, func(i, j int) bool {
		return refs[i].imageRef < refs[j].imageRef
	})
	return refs
}

func normalizeOCIConfigRevision(labels map[string]string) string {
	for _, key := range []string{
		"org.opencontainers.image.revision",
		"org.label-schema.vcs-ref",
	} {
		if revision := strings.TrimSpace(labels[key]); revision != "" && revision != "[redacted]" {
			return strings.ToLower(revision)
		}
	}
	return ""
}
