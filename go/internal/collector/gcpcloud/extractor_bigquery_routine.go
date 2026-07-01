// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// assetTypeBigQueryConnection is the CAI asset type for a BigQuery connection,
// the remote-function connection edge target. assetTypeBigQueryRoutine,
// assetTypeBigQueryDataset, and bigQueryResourceNamePrefix are already declared
// by the BigQuery Table/Dataset extractors.
const (
	assetTypeBigQueryConnection      = "bigqueryconnection.googleapis.com/Connection"
	bigQueryConnectionResourcePrefix = "//bigqueryconnection.googleapis.com/"
)

// Bounded provider relationship types for BigQuery Routine edges. Referenced
// tables are intentionally absent: a routine's table references live inside its
// definitionBody (SQL/JS source), which is never read, so only the structured
// parent-dataset and remote-function connection references become edges.
const (
	relationshipTypeRoutineInDataset      = "bigquery_routine_in_dataset"
	relationshipTypeRoutineUsesConnection = "bigquery_routine_uses_connection"
)

func init() {
	RegisterAssetExtractor(assetTypeBigQueryRoutine, extractBigQueryRoutine)
}

// jsonValuePresent records only whether a JSON field was present with a
// non-empty, non-null value, discarding the value itself during unmarshal. It is
// used for the routine definitionBody (user SQL/JavaScript source) so the body
// is never retained in memory — only its presence — eliminating any risk of it
// leaking through a future debug/trace of the decoded struct.
type jsonValuePresent bool

// UnmarshalJSON sets the flag true for any JSON value other than null or an
// empty string, without keeping the decoded bytes.
func (p *jsonValuePresent) UnmarshalJSON(b []byte) error {
	t := bytes.TrimSpace(b)
	*p = jsonValuePresent(len(t) > 0 && string(t) != "null" && string(t) != `""`)
	return nil
}

// bigQueryRoutineData is the bounded view of a CAI bigquery.googleapis.com/Routine
// resource.data blob. The definitionBody (user-authored SQL/JavaScript source)
// is decoded only to record its presence and is never persisted; argument and
// imported-library entries are counted, never stored, since they can carry
// user-supplied names and object paths.
type bigQueryRoutineData struct {
	RoutineReference *struct {
		ProjectID string `json:"projectId"`
		DatasetID string `json:"datasetId"`
	} `json:"routineReference"`
	RoutineType string            `json:"routineType"`
	Language    string            `json:"language"`
	Arguments   []json.RawMessage `json:"arguments"`
	ReturnType  *struct {
		TypeKind string `json:"typeKind"`
	} `json:"returnType"`
	DefinitionBody        jsonValuePresent `json:"definitionBody"`
	ImportedLibraries     []string         `json:"importedLibraries"`
	RemoteFunctionOptions *struct {
		Connection string `json:"connection"`
	} `json:"remoteFunctionOptions"`
	CreationTime string `json:"creationTime"`
}

// extractBigQueryRoutine extracts bounded, redaction-safe typed depth for one
// BigQuery Routine CAI asset. It returns the Terraform/drift/monitoring attribute
// set (routine type, language, argument count, return type kind, definition-body
// presence, imported-library count, creation time) and the typed parent-dataset
// and remote-function connection edges. The routine's source body is never read.
func extractBigQueryRoutine(ctx ExtractContext) (AttributeExtraction, error) {
	var data bigQueryRoutineData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode bigquery routine data: %w", err)
	}

	attrs := bigQueryRoutineAttributes(data)

	var anchors []string
	var rels []RelationshipObservation
	if dataset := bigQueryRoutineDatasetFullName(ctx, data); dataset != "" {
		anchors = append(anchors, dataset)
		rels = append(rels, bigQueryRoutineEdge(ctx, relationshipTypeRoutineInDataset, dataset, assetTypeBigQueryDataset))
	}
	if data.RemoteFunctionOptions != nil {
		if conn := bigQueryConnectionFullName(data.RemoteFunctionOptions.Connection); conn != "" {
			anchors = append(anchors, conn)
			rels = append(rels, bigQueryRoutineEdge(ctx, relationshipTypeRoutineUsesConnection, conn, assetTypeBigQueryConnection))
		}
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// bigQueryRoutineAttributes assembles the bounded attribute map. Empty or absent
// fields are omitted rather than written as zero values so a partial CAI page
// does not fabricate a posture.
func bigQueryRoutineAttributes(data bigQueryRoutineData) map[string]any {
	attrs := map[string]any{}

	if v := strings.TrimSpace(data.RoutineType); v != "" {
		attrs["routine_type"] = v
	}
	if v := strings.TrimSpace(data.Language); v != "" {
		attrs["language"] = v
	}
	if n := len(data.Arguments); n > 0 {
		attrs["argument_count"] = n
	}
	if data.ReturnType != nil {
		if v := strings.TrimSpace(data.ReturnType.TypeKind); v != "" {
			attrs["return_type_kind"] = v
		}
	}
	if bool(data.DefinitionBody) {
		attrs["has_definition_body"] = true
	}
	if n := len(data.ImportedLibraries); n > 0 {
		attrs["imported_library_count"] = n
	}
	if v := strings.TrimSpace(data.CreationTime); v != "" {
		attrs["creation_time"] = v
	}
	return attrs
}

// bigQueryRoutineDatasetFullName derives the parent dataset full resource name
// from the routine reference, falling back to trimming the routine segment from
// the routine's own full resource name when the reference is incomplete.
func bigQueryRoutineDatasetFullName(ctx ExtractContext, data bigQueryRoutineData) string {
	project := ctx.ProjectID
	var dataset string
	if data.RoutineReference != nil {
		project = firstNonEmpty(data.RoutineReference.ProjectID, ctx.ProjectID)
		dataset = strings.TrimSpace(data.RoutineReference.DatasetID)
	}
	if project != "" && dataset != "" {
		return fmt.Sprintf("%sprojects/%s/datasets/%s", bigQueryResourceNamePrefix, project, dataset)
	}
	if idx := strings.Index(ctx.FullResourceName, "/routines/"); idx > 0 {
		return ctx.FullResourceName[:idx]
	}
	return ""
}

// bigQueryConnectionFullName builds the CAI Connection full resource name from a
// remote-function connection reference (projects/.../connections/...). An
// already-normalized CAI full resource name is returned unchanged. It returns ""
// for a blank reference or one that does not name a connection.
func bigQueryConnectionFullName(connectionRef string) string {
	trimmed := strings.TrimSpace(connectionRef)
	if trimmed == "" || !strings.Contains(trimmed, "/connections/") {
		return ""
	}
	if strings.HasPrefix(trimmed, "//") {
		return trimmed
	}
	return bigQueryConnectionResourcePrefix + strings.TrimPrefix(trimmed, "/")
}

// bigQueryRoutineEdge builds one typed provider relationship observation anchored
// on the routine's CAI full resource name.
func bigQueryRoutineEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
