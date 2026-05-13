package cypher

import (
	"fmt"
	"sort"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

const canonicalPhasePackageRegistry = "package_registry"

const canonicalPackageRegistryPackageUpsertCypher = `UNWIND $rows AS row
MERGE (p:Package:PackageRegistryPackage {uid: row.uid})
SET p.id = row.uid,
    p.name = row.normalized_name,
    p.ecosystem = row.ecosystem,
    p.registry = row.registry,
    p.raw_name = row.raw_name,
    p.normalized_name = row.normalized_name,
    p.namespace = row.namespace,
    p.classifier = row.classifier,
    p.visibility = row.visibility,
    p.source_fact_id = row.source_fact_id,
    p.stable_fact_key = row.stable_fact_key,
    p.source_system = row.source_system,
    p.source_record_id = row.source_record_id,
    p.source_confidence = row.source_confidence,
    p.collector_kind = row.collector_kind,
    p.collector_instance_id = row.collector_instance_id,
    p.correlation_anchors = row.correlation_anchors,
    p.scope_id = row.scope_id,
    p.generation_id = row.generation_id,
    p.evidence_source = 'projector/package_registry'`

const canonicalPackageRegistryVersionUpsertCypher = `UNWIND $rows AS row
MATCH (p:Package {uid: row.package_id})
MERGE (v:PackageVersion:PackageRegistryPackageVersion {uid: row.uid})
SET v.id = row.uid,
    v.name = row.version,
    v.package_id = row.package_id,
    v.ecosystem = row.ecosystem,
    v.registry = row.registry,
    v.version = row.version,
    v.published_at = row.published_at,
    v.is_yanked = row.is_yanked,
    v.is_unlisted = row.is_unlisted,
    v.is_deprecated = row.is_deprecated,
    v.is_retracted = row.is_retracted,
    v.artifact_urls = row.artifact_urls,
    v.checksum_algorithms = row.checksum_algorithms,
    v.source_fact_id = row.source_fact_id,
    v.stable_fact_key = row.stable_fact_key,
    v.source_system = row.source_system,
    v.source_record_id = row.source_record_id,
    v.source_confidence = row.source_confidence,
    v.collector_kind = row.collector_kind,
    v.collector_instance_id = row.collector_instance_id,
    v.correlation_anchors = row.correlation_anchors,
    v.scope_id = row.scope_id,
    v.generation_id = row.generation_id,
    v.evidence_source = 'projector/package_registry'
MERGE (p)-[rel:HAS_VERSION]->(v)
SET rel.generation_id = row.generation_id,
    rel.evidence_source = 'projector/package_registry'`

func (w *CanonicalNodeWriter) buildPackageRegistryStatements(mat projector.CanonicalMaterialization) []Statement {
	var statements []Statement
	statements = append(
		statements,
		packageRegistryBatchedStatements(
			canonicalPackageRegistryPackageUpsertCypher,
			packageRegistryPackageRows(mat),
			w.batchSize,
			"PackageRegistryPackage",
			mat,
		)...,
	)
	statements = append(
		statements,
		packageRegistryBatchedStatements(
			canonicalPackageRegistryVersionUpsertCypher,
			packageRegistryVersionRows(mat),
			w.batchSize,
			"PackageRegistryPackageVersion",
			mat,
		)...,
	)
	return statements
}

func packageRegistryBatchedStatements(
	cypher string,
	rows []map[string]any,
	batchSize int,
	label string,
	mat projector.CanonicalMaterialization,
) []Statement {
	statements := buildBatchedStatements(cypher, rows, batchSize)
	for index := range statements {
		batchRows := statements[index].Parameters["rows"].([]map[string]any)
		statements[index].Parameters[StatementMetadataPhaseKey] = canonicalPhasePackageRegistry
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

func packageRegistryPackageRows(mat projector.CanonicalMaterialization) []map[string]any {
	rows := make([]map[string]any, 0, len(mat.PackageRegistryPackages))
	for _, row := range mat.PackageRegistryPackages {
		rows = append(rows, map[string]any{
			"uid":                   row.UID,
			"ecosystem":             row.Ecosystem,
			"registry":              row.Registry,
			"raw_name":              row.RawName,
			"normalized_name":       row.NormalizedName,
			"namespace":             row.Namespace,
			"classifier":            row.Classifier,
			"visibility":            row.Visibility,
			"source_fact_id":        row.SourceFactID,
			"stable_fact_key":       row.StableFactKey,
			"source_system":         row.SourceSystem,
			"source_record_id":      row.SourceRecordID,
			"source_confidence":     row.SourceConfidence,
			"collector_kind":        row.CollectorKind,
			"collector_instance_id": row.CollectorInstanceID,
			"correlation_anchors":   row.CorrelationAnchors,
			"scope_id":              mat.ScopeID,
			"generation_id":         mat.GenerationID,
		})
	}
	return rows
}

func packageRegistryVersionRows(mat projector.CanonicalMaterialization) []map[string]any {
	rows := make([]map[string]any, 0, len(mat.PackageRegistryVersions))
	for _, row := range mat.PackageRegistryVersions {
		rows = append(rows, map[string]any{
			"uid":                   row.UID,
			"package_id":            row.PackageID,
			"ecosystem":             row.Ecosystem,
			"registry":              row.Registry,
			"version":               row.Version,
			"published_at":          packageRegistryTimeValue(row.PublishedAt),
			"is_yanked":             row.IsYanked,
			"is_unlisted":           row.IsUnlisted,
			"is_deprecated":         row.IsDeprecated,
			"is_retracted":          row.IsRetracted,
			"artifact_urls":         row.ArtifactURLs,
			"checksum_algorithms":   checksumAlgorithms(row.Checksums),
			"source_fact_id":        row.SourceFactID,
			"stable_fact_key":       row.StableFactKey,
			"source_system":         row.SourceSystem,
			"source_record_id":      row.SourceRecordID,
			"source_confidence":     row.SourceConfidence,
			"collector_kind":        row.CollectorKind,
			"collector_instance_id": row.CollectorInstanceID,
			"correlation_anchors":   row.CorrelationAnchors,
			"scope_id":              mat.ScopeID,
			"generation_id":         mat.GenerationID,
		})
	}
	return rows
}

func packageRegistryTimeValue(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

func checksumAlgorithms(checksums map[string]string) []string {
	if len(checksums) == 0 {
		return nil
	}
	algorithms := make([]string, 0, len(checksums))
	for algorithm := range checksums {
		algorithms = append(algorithms, algorithm)
	}
	sort.Strings(algorithms)
	return algorithms
}
