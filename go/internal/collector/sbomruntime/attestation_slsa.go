// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbomruntime

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/workflow"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	sbomv1 "github.com/eshu-hq/eshu/sdk/go/factschema/sbom/v1"
)

// slsaProvenancePredicateTypes is the CLOSED set of SLSA provenance
// predicateType URIs this collector decodes. Membership requires an exact
// match (after strings.TrimSpace), never a substring match on
// "slsa.dev/provenance": a future or unrecognized SLSA-shaped URI must fall
// through as statement-only evidence, not be guessed at with the wrong
// builder.id path.
var slsaProvenancePredicateTypes = map[string]bool{
	"https://slsa.dev/provenance/v1":   true,
	"https://slsa.dev/provenance/v0.2": true,
	"https://slsa.dev/provenance/v0.1": true,
}

// attestationSLSAProvenanceEnvelopes extracts a SLSA provenance predicate
// from a successfully parsed in-toto statement. It returns nil when the
// statement's predicateType is not in the closed slsaProvenancePredicateTypes
// set (no SLSA evidence to extract). When the predicate type matches but the
// predicate itself is absent, JSON null, or fails to decode into its
// version's builder shape, it returns a single sbom.warning envelope
// (reason "malformed_slsa_predicate") instead of a provenance fact, so an
// undecodable predicate never silently becomes an empty-but-present fact. On
// a successful decode it returns a single attestation.slsa_provenance
// envelope; BuilderID is left unset when the predicate is well-formed but
// reports no builder.id.
func attestationSLSAProvenanceEnvelopes(
	item workflow.WorkItem,
	target TargetConfig,
	statement inTotoStatement,
	statementID string,
	statementDigest string,
	sourceURI string,
	sourceRecordID string,
	observedAt time.Time,
) []facts.Envelope {
	predicateType := strings.TrimSpace(statement.PredicateType)
	if !slsaProvenancePredicateTypes[predicateType] {
		return nil
	}
	details, ok := decodeSLSAProvenanceDetails(predicateType, statement.Predicate)
	if !ok {
		return []facts.Envelope{attestationMalformedSLSAPredicateWarningEnvelope(
			item, target, statementID, sourceURI, sourceRecordID, observedAt,
		)}
	}
	return []facts.Envelope{attestationSLSAProvenanceEnvelope(
		item, target, statementID, statementDigest, predicateType, details, sourceURI, sourceRecordID, observedAt,
	)}
}

// slsaProvenanceDetails carries every field decodeSLSAProvenanceDetails
// extracts from a SLSA provenance predicate at its version's field path.
// BuilderID stays empty (not a pointer) so the caller's existing
// builderID != "" gating is unchanged; Materials/ConfigSource are the typed
// #5456 additions and are left nil/zero when the predicate reports none.
type slsaProvenanceDetails struct {
	builderID    string
	materials    []sbomv1.SLSAMaterial
	configSource *sbomv1.SLSAConfigSource
}

// slsaResourceDescriptor is the shared shape in-toto/SLSA uses for a
// material/resolved-dependency entry and a configSource: a URI plus a
// digest-algorithm map, with configSource adding an entryPoint. It decodes
// materials, resolvedDependencies, and configSource across every predicate
// version (#5456); entryPoint simply stays empty for a materials entry,
// which never carries that key.
type slsaResourceDescriptor struct {
	URI        string            `json:"uri"`
	Digest     map[string]string `json:"digest"`
	EntryPoint string            `json:"entryPoint"`
}

func (d slsaResourceDescriptor) material() sbomv1.SLSAMaterial {
	material := sbomv1.SLSAMaterial{}
	if uri := strings.TrimSpace(d.URI); uri != "" {
		material.URI = stringPtr(uri)
	}
	if len(d.Digest) > 0 {
		material.Digest = d.Digest
	}
	return material
}

// decodeSLSAProvenanceDetails decodes a SLSA provenance predicate's builder
// identity, materials, and config source at the field path its predicateType
// version defines:
//
//   - v1 (https://slsa.dev/provenance/v1): builder identity at
//     runDetails.builder.id; config source at
//     buildDefinition.externalParameters.configSource; materials from
//     buildDefinition.resolvedDependencies[].
//   - v0.2/v0.1: builder identity at the top-level builder.id; config source
//     at invocation.configSource; materials from the top-level materials[].
//
// It returns ok=false for an absent predicate (empty after TrimSpace), a
// literal JSON null, or a predicate that fails to unmarshal into its
// version's shape — Go decodes a JSON null into a zero-value struct WITHOUT
// an error, so the null check must be explicit or a null predicate would
// silently fabricate an empty-but-present fact. A well-formed predicate with
// no builder.id/configSource/materials at its version's path returns ok=true
// with those fields left empty/nil, which the caller leaves unset on the
// fact rather than encoding zero values.
func decodeSLSAProvenanceDetails(predicateType string, predicate json.RawMessage) (slsaProvenanceDetails, bool) {
	trimmed := strings.TrimSpace(string(predicate))
	if trimmed == "" || trimmed == "null" {
		return slsaProvenanceDetails{}, false
	}
	switch predicateType {
	case "https://slsa.dev/provenance/v1":
		var decoded struct {
			BuildDefinition struct {
				ExternalParameters struct {
					ConfigSource slsaResourceDescriptor `json:"configSource"`
				} `json:"externalParameters"`
				ResolvedDependencies []slsaResourceDescriptor `json:"resolvedDependencies"`
			} `json:"buildDefinition"`
			RunDetails struct {
				Builder struct {
					ID string `json:"id"`
				} `json:"builder"`
			} `json:"runDetails"`
		}
		if err := json.Unmarshal(predicate, &decoded); err != nil {
			return slsaProvenanceDetails{}, false
		}
		return slsaProvenanceDetails{
			builderID:    strings.TrimSpace(decoded.RunDetails.Builder.ID),
			materials:    slsaMaterialsFromDescriptors(decoded.BuildDefinition.ResolvedDependencies),
			configSource: slsaConfigSourceFromDescriptor(decoded.BuildDefinition.ExternalParameters.ConfigSource),
		}, true
	case "https://slsa.dev/provenance/v0.2", "https://slsa.dev/provenance/v0.1":
		var decoded struct {
			Builder struct {
				ID string `json:"id"`
			} `json:"builder"`
			Invocation struct {
				ConfigSource slsaResourceDescriptor `json:"configSource"`
			} `json:"invocation"`
			Materials []slsaResourceDescriptor `json:"materials"`
		}
		if err := json.Unmarshal(predicate, &decoded); err != nil {
			return slsaProvenanceDetails{}, false
		}
		return slsaProvenanceDetails{
			builderID:    strings.TrimSpace(decoded.Builder.ID),
			materials:    slsaMaterialsFromDescriptors(decoded.Materials),
			configSource: slsaConfigSourceFromDescriptor(decoded.Invocation.ConfigSource),
		}, true
	default:
		// Unreachable: attestationSLSAProvenanceEnvelopes only calls this for
		// a predicateType already confirmed in slsaProvenancePredicateTypes.
		return slsaProvenanceDetails{}, false
	}
}

// slsaMaterialsFromDescriptors converts decoded materials/resolvedDependencies
// entries into the typed sbomv1.SLSAMaterial list, dropping any entry with
// neither a URI nor a digest (an empty descriptor contributes no evidence).
func slsaMaterialsFromDescriptors(descriptors []slsaResourceDescriptor) []sbomv1.SLSAMaterial {
	materials := make([]sbomv1.SLSAMaterial, 0, len(descriptors))
	for _, descriptor := range descriptors {
		material := descriptor.material()
		if material.URI == nil && len(material.Digest) == 0 {
			continue
		}
		materials = append(materials, material)
	}
	if len(materials) == 0 {
		return nil
	}
	return materials
}

// slsaConfigSourceFromDescriptor converts a decoded configSource descriptor
// into the typed sbomv1.SLSAConfigSource, returning nil when the descriptor
// carries no URI, digest, or entry point (an absent configSource key decodes
// to a zero-value descriptor, which must not fabricate an empty-but-present
// config_source).
func slsaConfigSourceFromDescriptor(descriptor slsaResourceDescriptor) *sbomv1.SLSAConfigSource {
	uri := strings.TrimSpace(descriptor.URI)
	entryPoint := strings.TrimSpace(descriptor.EntryPoint)
	if uri == "" && len(descriptor.Digest) == 0 && entryPoint == "" {
		return nil
	}
	configSource := &sbomv1.SLSAConfigSource{}
	if uri != "" {
		configSource.URI = stringPtr(uri)
	}
	if len(descriptor.Digest) > 0 {
		configSource.Digest = descriptor.Digest
	}
	if entryPoint != "" {
		configSource.EntryPoint = stringPtr(entryPoint)
	}
	return configSource
}

func attestationSLSAProvenanceEnvelope(
	item workflow.WorkItem,
	target TargetConfig,
	statementID string,
	statementDigest string,
	predicateType string,
	details slsaProvenanceDetails,
	sourceURI string,
	sourceRecordID string,
	observedAt time.Time,
) facts.Envelope {
	payload := map[string]any{
		"statement_id":   statementID,
		"predicate_type": predicateType,
	}
	var builderIDPtr *string
	if details.builderID != "" {
		payload["builder_id"] = details.builderID
		builderIDPtr = stringPtr(details.builderID)
	}
	if len(details.materials) > 0 {
		payload["materials"] = details.materials
	}
	if details.configSource != nil {
		payload["config_source"] = details.configSource
	}
	mergeContractPayloadNoError(payload, func() (map[string]any, error) {
		return factschema.EncodeAttestationSLSAProvenance(sbomv1.SLSAProvenance{
			StatementID:   statementID,
			PredicateType: stringPtr(predicateType),
			BuilderID:     builderIDPtr,
			Materials:     details.materials,
			ConfigSource:  details.configSource,
		})
	})
	stableKey := facts.StableID(facts.AttestationSLSAProvenanceFactKind, map[string]any{
		"statement_digest": statementDigest,
		"statement_id":     statementID,
	})
	return runtimeEnvelope(item, target, facts.AttestationSLSAProvenanceFactKind, stableKey, sourceRecordID, sourceURI, observedAt, payload)
}

func attestationMalformedSLSAPredicateWarningEnvelope(
	item workflow.WorkItem,
	target TargetConfig,
	statementID string,
	sourceURI string,
	sourceRecordID string,
	observedAt time.Time,
) facts.Envelope {
	payload := map[string]any{
		"statement_id": statementID,
		"reason":       "malformed_slsa_predicate",
		"summary":      "SLSA provenance predicate could not be decoded",
	}
	mergeContractPayloadNoError(payload, func() (map[string]any, error) {
		return factschema.EncodeSBOMWarning(sbomv1.Warning{
			StatementID: stringPtr(statementID),
			Reason:      stringPtr("malformed_slsa_predicate"),
			Summary:     stringPtr("SLSA provenance predicate could not be decoded"),
		})
	})
	stableKey := facts.StableID(facts.SBOMWarningFactKind, map[string]any{
		"reason":       "malformed_slsa_predicate",
		"statement_id": statementID,
	})
	return runtimeEnvelope(item, target, facts.SBOMWarningFactKind, stableKey, sourceRecordID, sourceURI, observedAt, payload)
}
