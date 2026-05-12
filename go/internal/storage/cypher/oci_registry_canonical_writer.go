package cypher

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

const canonicalPhaseOCIRegistry = "oci_registry"

const canonicalOCIRegistryRepositoryUpsertCypher = `UNWIND $rows AS row
MERGE (r:OciRegistryRepository {uid: row.uid})
SET r.id = row.uid,
    r.name = row.repository,
    r.provider = row.provider,
    r.registry = row.registry,
    r.repository = row.repository,
    r.visibility = row.visibility,
    r.auth_mode = row.auth_mode,
    r.source_fact_id = row.source_fact_id,
    r.stable_fact_key = row.stable_fact_key,
    r.source_system = row.source_system,
    r.source_record_id = row.source_record_id,
    r.source_confidence = row.source_confidence,
    r.collector_kind = row.collector_kind,
    r.scope_id = row.scope_id,
    r.generation_id = row.generation_id,
    r.evidence_source = 'projector/oci_registry'`

const canonicalOCIImageManifestUpsertCypher = `UNWIND $rows AS row
MATCH (r:OciRegistryRepository {uid: row.repository_id})
MERGE (m:ContainerImage:OciImageManifest {uid: row.uid})
SET m.id = row.uid,
    m.name = row.digest,
    m.repository_id = row.repository_id,
    m.digest = row.digest,
    m.media_type = row.media_type,
    m.size_bytes = row.size_bytes,
    m.artifact_type = row.artifact_type,
    m.source_tag = row.source_tag,
    m.config_digest = row.config_digest,
    m.layer_digests = row.layer_digests,
    m.source_fact_id = row.source_fact_id,
    m.stable_fact_key = row.stable_fact_key,
    m.source_system = row.source_system,
    m.source_record_id = row.source_record_id,
    m.source_confidence = row.source_confidence,
    m.collector_kind = row.collector_kind,
    m.collector_instance_id = row.collector_instance_id,
    m.correlation_anchors = row.correlation_anchors,
    m.scope_id = row.scope_id,
    m.generation_id = row.generation_id,
    m.evidence_source = 'projector/oci_registry'
MERGE (r)-[rel:PUBLISHES_MANIFEST]->(m)
SET rel.generation_id = row.generation_id,
    rel.evidence_source = 'projector/oci_registry'`

const canonicalOCIImageIndexUpsertCypher = `UNWIND $rows AS row
MATCH (r:OciRegistryRepository {uid: row.repository_id})
MERGE (i:ContainerImageIndex:OciImageIndex {uid: row.uid})
SET i.id = row.uid,
    i.name = row.digest,
    i.repository_id = row.repository_id,
    i.digest = row.digest,
    i.media_type = row.media_type,
    i.size_bytes = row.size_bytes,
    i.artifact_type = row.artifact_type,
    i.manifest_digests = row.manifest_digests,
    i.source_fact_id = row.source_fact_id,
    i.stable_fact_key = row.stable_fact_key,
    i.source_system = row.source_system,
    i.source_record_id = row.source_record_id,
    i.source_confidence = row.source_confidence,
    i.collector_kind = row.collector_kind,
    i.correlation_anchors = row.correlation_anchors,
    i.scope_id = row.scope_id,
    i.generation_id = row.generation_id,
    i.evidence_source = 'projector/oci_registry'
MERGE (r)-[rel:PUBLISHES_INDEX]->(i)
SET rel.generation_id = row.generation_id,
    rel.evidence_source = 'projector/oci_registry'`

const canonicalOCIImageDescriptorUpsertCypher = `UNWIND $rows AS row
MATCH (r:OciRegistryRepository {uid: row.repository_id})
MERGE (d:ContainerImageDescriptor:OciImageDescriptor {uid: row.uid})
SET d.id = row.uid,
    d.name = row.digest,
    d.repository_id = row.repository_id,
    d.digest = row.digest,
    d.media_type = row.media_type,
    d.size_bytes = row.size_bytes,
    d.artifact_type = row.artifact_type,
    d.source_fact_id = row.source_fact_id,
    d.stable_fact_key = row.stable_fact_key,
    d.source_system = row.source_system,
    d.source_record_id = row.source_record_id,
    d.source_confidence = row.source_confidence,
    d.collector_kind = row.collector_kind,
    d.scope_id = row.scope_id,
    d.generation_id = row.generation_id,
    d.evidence_source = 'projector/oci_registry'
MERGE (r)-[rel:PUBLISHES_DESCRIPTOR]->(d)
SET rel.generation_id = row.generation_id,
    rel.evidence_source = 'projector/oci_registry'`

const canonicalOCIImageTagObservationUpsertCypher = `UNWIND $rows AS row
MATCH (r:OciRegistryRepository {uid: row.repository_id})
MERGE (t:ContainerImageTagObservation:OciImageTagObservation {uid: row.uid})
SET t.id = row.uid,
    t.name = row.tag,
    t.repository_id = row.repository_id,
    t.image_ref = row.image_ref,
    t.reference = row.image_ref,
    t.tag = row.tag,
    t.resolved_digest = row.resolved_digest,
    t.resolved_descriptor_uid = row.resolved_descriptor_uid,
    t.media_type = row.media_type,
    t.previous_digest = row.previous_digest,
    t.mutated = row.mutated,
    t.identity_strength = row.identity_strength,
    t.source_fact_id = row.source_fact_id,
    t.stable_fact_key = row.stable_fact_key,
    t.source_system = row.source_system,
    t.source_record_id = row.source_record_id,
    t.source_confidence = row.source_confidence,
    t.collector_kind = row.collector_kind,
    t.scope_id = row.scope_id,
    t.generation_id = row.generation_id,
    t.evidence_source = 'projector/oci_registry'
MERGE (r)-[rel:OBSERVED_TAG]->(t)
SET rel.confidence = 0.5,
    rel.reason = 'Registry tag is mutable evidence for a digest observation',
    rel.generation_id = row.generation_id,
    rel.evidence_source = 'projector/oci_registry'`

const canonicalOCIImageReferrerUpsertCypher = `UNWIND $rows AS row
MATCH (r:OciRegistryRepository {uid: row.repository_id})
MERGE (ref:OciImageReferrer {uid: row.uid})
SET ref.id = row.uid,
    ref.name = row.referrer_digest,
    ref.repository_id = row.repository_id,
    ref.subject_digest = row.subject_digest,
    ref.subject_media_type = row.subject_media_type,
    ref.referrer_digest = row.referrer_digest,
    ref.referrer_media_type = row.referrer_media_type,
    ref.artifact_type = row.artifact_type,
    ref.size_bytes = row.size_bytes,
    ref.source_api_path = row.source_api_path,
    ref.source_fact_id = row.source_fact_id,
    ref.stable_fact_key = row.stable_fact_key,
    ref.source_system = row.source_system,
    ref.source_record_id = row.source_record_id,
    ref.source_confidence = row.source_confidence,
    ref.collector_kind = row.collector_kind,
    ref.scope_id = row.scope_id,
    ref.generation_id = row.generation_id,
    ref.evidence_source = 'projector/oci_registry'
MERGE (r)-[rel:OBSERVED_REFERRER]->(ref)
SET rel.generation_id = row.generation_id,
    rel.evidence_source = 'projector/oci_registry'`

func (w *CanonicalNodeWriter) buildOCIRegistryStatements(mat projector.CanonicalMaterialization) []Statement {
	var statements []Statement
	if mat.OCIRegistryRepository != nil {
		statements = append(
			statements,
			ociRegistryBatchedStatements(
				canonicalOCIRegistryRepositoryUpsertCypher,
				ociRegistryRepositoryRows(mat),
				w.batchSize,
				"OciRegistryRepository",
				mat,
			)...,
		)
	}
	statements = append(
		statements,
		ociRegistryBatchedStatements(
			canonicalOCIImageManifestUpsertCypher,
			ociImageManifestRows(mat),
			w.batchSize,
			"OciImageManifest",
			mat,
		)...,
	)
	statements = append(
		statements,
		ociRegistryBatchedStatements(
			canonicalOCIImageIndexUpsertCypher,
			ociImageIndexRows(mat),
			w.batchSize,
			"OciImageIndex",
			mat,
		)...,
	)
	statements = append(
		statements,
		ociRegistryBatchedStatements(
			canonicalOCIImageDescriptorUpsertCypher,
			ociImageDescriptorRows(mat),
			w.batchSize,
			"OciImageDescriptor",
			mat,
		)...,
	)
	statements = append(
		statements,
		ociRegistryBatchedStatements(
			canonicalOCIImageTagObservationUpsertCypher,
			ociImageTagObservationRows(mat),
			w.batchSize,
			"OciImageTagObservation",
			mat,
		)...,
	)
	statements = append(
		statements,
		ociRegistryBatchedStatements(
			canonicalOCIImageReferrerUpsertCypher,
			ociImageReferrerRows(mat),
			w.batchSize,
			"OciImageReferrer",
			mat,
		)...,
	)
	return statements
}

func ociRegistryBatchedStatements(
	cypher string,
	rows []map[string]any,
	batchSize int,
	label string,
	mat projector.CanonicalMaterialization,
) []Statement {
	statements := buildBatchedStatements(cypher, rows, batchSize)
	for index := range statements {
		batchRows := statements[index].Parameters["rows"].([]map[string]any)
		statements[index].Parameters[StatementMetadataPhaseKey] = canonicalPhaseOCIRegistry
		statements[index].Parameters[StatementMetadataEntityLabelKey] = label
		statements[index].Parameters[StatementMetadataScopeIDKey] = mat.ScopeID
		statements[index].Parameters[StatementMetadataGenerationIDKey] = mat.GenerationID
		statements[index].Parameters[StatementMetadataSummaryKey] = fmt.Sprintf(
			"label=%s rows=%d",
			label,
			len(batchRows),
		)
	}
	return statements
}

func ociRegistryRepositoryRows(mat projector.CanonicalMaterialization) []map[string]any {
	if mat.OCIRegistryRepository == nil {
		return nil
	}
	row := mat.OCIRegistryRepository
	return []map[string]any{{
		"uid":               row.UID,
		"provider":          row.Provider,
		"registry":          row.Registry,
		"repository":        row.Repository,
		"visibility":        row.Visibility,
		"auth_mode":         row.AuthMode,
		"source_fact_id":    row.SourceFactID,
		"stable_fact_key":   row.StableFactKey,
		"source_system":     row.SourceSystem,
		"source_record_id":  row.SourceRecordID,
		"source_confidence": row.SourceConfidence,
		"collector_kind":    row.CollectorKind,
		"scope_id":          mat.ScopeID,
		"generation_id":     mat.GenerationID,
	}}
}

func ociImageManifestRows(mat projector.CanonicalMaterialization) []map[string]any {
	rows := make([]map[string]any, 0, len(mat.OCIImageManifests))
	for _, row := range mat.OCIImageManifests {
		rows = append(rows, map[string]any{
			"uid":                   row.UID,
			"repository_id":         row.RepositoryID,
			"digest":                row.Digest,
			"media_type":            row.MediaType,
			"size_bytes":            row.SizeBytes,
			"artifact_type":         row.ArtifactType,
			"source_tag":            row.SourceTag,
			"config_digest":         row.ConfigDigest,
			"layer_digests":         row.LayerDigests,
			"source_fact_id":        row.SourceFactID,
			"stable_fact_key":       row.StableFactKey,
			"source_system":         row.SourceSystem,
			"source_record_id":      row.SourceRecordID,
			"source_confidence":     row.SourceConfidence,
			"collector_kind":        row.CollectorKind,
			"correlation_anchors":   row.CorrelationAnchors,
			"collector_instance_id": row.CollectorInstanceID,
			"scope_id":              mat.ScopeID,
			"generation_id":         mat.GenerationID,
		})
	}
	return rows
}

func ociImageIndexRows(mat projector.CanonicalMaterialization) []map[string]any {
	rows := make([]map[string]any, 0, len(mat.OCIImageIndexes))
	for _, row := range mat.OCIImageIndexes {
		rows = append(rows, map[string]any{
			"uid":                 row.UID,
			"repository_id":       row.RepositoryID,
			"digest":              row.Digest,
			"media_type":          row.MediaType,
			"size_bytes":          row.SizeBytes,
			"artifact_type":       row.ArtifactType,
			"manifest_digests":    row.ManifestDigests,
			"source_fact_id":      row.SourceFactID,
			"stable_fact_key":     row.StableFactKey,
			"source_system":       row.SourceSystem,
			"source_record_id":    row.SourceRecordID,
			"source_confidence":   row.SourceConfidence,
			"collector_kind":      row.CollectorKind,
			"correlation_anchors": row.CorrelationAnchors,
			"scope_id":            mat.ScopeID,
			"generation_id":       mat.GenerationID,
		})
	}
	return rows
}

func ociImageDescriptorRows(mat projector.CanonicalMaterialization) []map[string]any {
	rows := make([]map[string]any, 0, len(mat.OCIImageDescriptors))
	for _, row := range mat.OCIImageDescriptors {
		rows = append(rows, map[string]any{
			"uid":               row.UID,
			"repository_id":     row.RepositoryID,
			"digest":            row.Digest,
			"media_type":        row.MediaType,
			"size_bytes":        row.SizeBytes,
			"artifact_type":     row.ArtifactType,
			"source_fact_id":    row.SourceFactID,
			"stable_fact_key":   row.StableFactKey,
			"source_system":     row.SourceSystem,
			"source_record_id":  row.SourceRecordID,
			"source_confidence": row.SourceConfidence,
			"collector_kind":    row.CollectorKind,
			"scope_id":          mat.ScopeID,
			"generation_id":     mat.GenerationID,
		})
	}
	return rows
}

func ociImageTagObservationRows(mat projector.CanonicalMaterialization) []map[string]any {
	rows := make([]map[string]any, 0, len(mat.OCIImageTagObservations))
	for _, row := range mat.OCIImageTagObservations {
		rows = append(rows, map[string]any{
			"uid":                     row.UID,
			"repository_id":           row.RepositoryID,
			"image_ref":               row.ImageRef,
			"tag":                     row.Tag,
			"resolved_digest":         row.ResolvedDigest,
			"resolved_descriptor_uid": row.ResolvedDescriptorUID,
			"media_type":              row.MediaType,
			"previous_digest":         row.PreviousDigest,
			"mutated":                 row.Mutated,
			"identity_strength":       row.IdentityStrength,
			"source_fact_id":          row.SourceFactID,
			"stable_fact_key":         row.StableFactKey,
			"source_system":           row.SourceSystem,
			"source_record_id":        row.SourceRecordID,
			"source_confidence":       row.SourceConfidence,
			"collector_kind":          row.CollectorKind,
			"scope_id":                mat.ScopeID,
			"generation_id":           mat.GenerationID,
		})
	}
	return rows
}

func ociImageReferrerRows(mat projector.CanonicalMaterialization) []map[string]any {
	rows := make([]map[string]any, 0, len(mat.OCIImageReferrers))
	for _, row := range mat.OCIImageReferrers {
		rows = append(rows, map[string]any{
			"uid":                 row.UID,
			"repository_id":       row.RepositoryID,
			"subject_digest":      row.SubjectDigest,
			"subject_media_type":  row.SubjectMediaType,
			"referrer_digest":     row.ReferrerDigest,
			"referrer_media_type": row.ReferrerMediaType,
			"artifact_type":       row.ArtifactType,
			"size_bytes":          row.SizeBytes,
			"source_api_path":     row.SourceAPIPath,
			"source_fact_id":      row.SourceFactID,
			"stable_fact_key":     row.StableFactKey,
			"source_system":       row.SourceSystem,
			"source_record_id":    row.SourceRecordID,
			"source_confidence":   row.SourceConfidence,
			"collector_kind":      row.CollectorKind,
			"scope_id":            mat.ScopeID,
			"generation_id":       mat.GenerationID,
		})
	}
	return rows
}
