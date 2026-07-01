// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Bounded provider relationship types for BigQuery Dataset edges. Each is a
// stable string carried on a gcp_cloud_relationship fact; the reducer
// materializes an edge only when both endpoints resolve exactly.
const (
	// relationshipTypeBigQueryDatasetKMSKey mirrors the Table KMS edge: the
	// dataset is default-encrypted by a Cloud KMS CryptoKey.
	relationshipTypeBigQueryDatasetKMSKey = "bigquery_dataset_encrypted_by_kms_key"
	// relationshipTypeBigQueryDatasetAuthorizesView is an authorized-view ACL
	// grant: this dataset authorizes a view (a BigQuery Table) to read it.
	relationshipTypeBigQueryDatasetAuthorizesView = "bigquery_dataset_authorizes_view"
	// relationshipTypeBigQueryDatasetAuthorizesDataset is an authorized-dataset
	// ACL grant between two datasets.
	relationshipTypeBigQueryDatasetAuthorizesDataset = "bigquery_dataset_authorizes_dataset"
	// relationshipTypeBigQueryDatasetAuthorizesRoutine is an authorized-routine
	// ACL grant to a BigQuery Routine.
	relationshipTypeBigQueryDatasetAuthorizesRoutine = "bigquery_dataset_authorizes_routine"
)

// assetTypeBigQueryRoutine is the CAI asset type for an authorized-routine ACL
// edge endpoint. assetTypeBigQueryTable and assetTypeBigQueryDataset are shared
// constants declared in extractor_bigquery_table.go (package-scoped by design).
const assetTypeBigQueryRoutine = "bigquery.googleapis.com/Routine"

// bigQueryDatasetData is the bounded view of a CAI bigquery.googleapis.com/Dataset
// resource.data blob. Only redaction-safe control-plane metadata is decoded: the
// default encryption key reference, expiration policies, timestamps, location,
// and the shape of the access list. Raw IAM member identities inside access[] are
// never retained — only the bounded role set and principal classes are kept.
// defaultTableExpirationMs, defaultPartitionExpirationMs, creationTime, and
// lastModifiedTime arrive as JSON strings in the BigQuery REST representation.
type bigQueryDatasetData struct {
	DatasetReference struct {
		ProjectID string `json:"projectId"`
		DatasetID string `json:"datasetId"`
	} `json:"datasetReference"`
	Location                     string `json:"location"`
	DefaultTableExpirationMs     string `json:"defaultTableExpirationMs"`
	DefaultPartitionExpirationMs string `json:"defaultPartitionExpirationMs"`
	CreationTime                 string `json:"creationTime"`
	LastModifiedTime             string `json:"lastModifiedTime"`
	DefaultEncryptionConfig      *struct {
		KMSKeyName string `json:"kmsKeyName"`
	} `json:"defaultEncryptionConfiguration"`
	Access []bigQueryDatasetAccessEntry `json:"access"`
}

// bigQueryDatasetAccessEntry is one bounded BigQuery dataset ACL entry. Exactly
// one principal field is set per entry in a well-formed policy; the raw email,
// group, or member identity is classified into a bounded principal class and
// then discarded so no IAM identity reaches the fact. The view/dataset/routine
// fields are authorized-resource references (not member identities): they name
// another BigQuery resource this dataset shares access with and are safe to keep
// as typed edge endpoints.
type bigQueryDatasetAccessEntry struct {
	Role         string                       `json:"role"`
	UserByEmail  string                       `json:"userByEmail"`
	GroupByEmail string                       `json:"groupByEmail"`
	Domain       string                       `json:"domain"`
	SpecialGroup string                       `json:"specialGroup"`
	IAMMember    string                       `json:"iamMember"`
	View         *bigQueryTableRef            `json:"view"`
	Routine      *bigQueryRoutineRef          `json:"routine"`
	Dataset      *bigQueryDatasetAccessTarget `json:"dataset"`
}

// bigQueryTableRef is a bounded reference to a BigQuery table or view.
type bigQueryTableRef struct {
	ProjectID string `json:"projectId"`
	DatasetID string `json:"datasetId"`
	TableID   string `json:"tableId"`
}

// bigQueryRoutineRef is a bounded reference to a BigQuery routine.
type bigQueryRoutineRef struct {
	ProjectID string `json:"projectId"`
	DatasetID string `json:"datasetId"`
	RoutineID string `json:"routineId"`
}

// bigQueryDatasetAccessTarget is the authorized-dataset ACL shape: the target
// dataset reference nested under `dataset`.
type bigQueryDatasetAccessTarget struct {
	Dataset struct {
		ProjectID string `json:"projectId"`
		DatasetID string `json:"datasetId"`
	} `json:"dataset"`
}

func init() {
	RegisterAssetExtractor(assetTypeBigQueryDataset, extractBigQueryDataset)
}

// extractBigQueryDataset extracts bounded typed depth for one BigQuery Dataset CAI
// asset. It returns the Terraform/drift/monitoring attribute set (location,
// expiration policies, timestamps, default encryption key, and a redaction-safe
// summary of the access ACL), correlation anchors, and typed edges: the
// default-encryption relationship to the Cloud KMS CryptoKey and one
// authorizes-view/dataset/routine edge per authorized-resource ACL entry
// (dataset-sharing dependencies). The dataset's own tables are not enumerable
// from its resource.data — the child Table extractor emits the parent edge — and
// IAM member identities (user/group/domain/serviceAccount) are never resolvable
// CAI endpoints, so those ACL entries stay class-summary only.
func extractBigQueryDataset(ctx ExtractContext) (AttributeExtraction, error) {
	var data bigQueryDatasetData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode bigquery dataset data: %w", err)
	}

	attrs := bigQueryDatasetAttributes(data)
	anchors := make([]string, 0, 2)
	rels := make([]RelationshipObservation, 0, 4)

	if kms := datasetEncryptionKMSKeyName(data); kms != "" {
		attrs["kms_key_name"] = kms
		kmsName := cloudKMSResourceNamePrefix + kms
		anchors = append(anchors, kmsName)
		rels = append(rels, bigQueryDatasetEdge(ctx, relationshipTypeBigQueryDatasetKMSKey, kmsName, assetTypeKMSCryptoKey))
	}

	for _, edge := range authorizedResourceEdges(ctx, data.Access) {
		anchors = append(anchors, edge.TargetFullResourceName)
		rels = append(rels, edge)
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// authorizedResourceEdges derives typed authorizes-view/dataset/routine edges
// from the dataset ACL. Each authorized-resource reference is a resolvable CAI
// endpoint (Table, Dataset, or Routine), so it becomes an edge and correlation
// anchor rather than an opaque member class. The project id falls back to the
// dataset's own project when the reference omits it. Edges are deduplicated by
// (relationship type, target) so a repeated grant does not double-count.
func authorizedResourceEdges(ctx ExtractContext, access []bigQueryDatasetAccessEntry) []RelationshipObservation {
	if len(access) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(access))
	edges := make([]RelationshipObservation, 0, len(access))
	add := func(relType, target, targetType string) {
		if target == "" {
			return
		}
		key := relType + "\x00" + target
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		edges = append(edges, bigQueryDatasetEdge(ctx, relType, target, targetType))
	}
	for _, entry := range access {
		if entry.View != nil {
			add(relationshipTypeBigQueryDatasetAuthorizesView,
				bigQueryAuthorizedViewFullName(ctx, entry.View), assetTypeBigQueryTable)
		}
		if entry.Routine != nil {
			add(relationshipTypeBigQueryDatasetAuthorizesRoutine,
				bigQueryRoutineFullName(ctx, entry.Routine), assetTypeBigQueryRoutine)
		}
		if entry.Dataset != nil {
			add(relationshipTypeBigQueryDatasetAuthorizesDataset,
				bigQueryAuthorizedDatasetFullName(ctx, entry.Dataset), assetTypeBigQueryDataset)
		}
	}
	return edges
}

// bigQueryAuthorizedViewFullName builds the CAI full resource name for an authorized view
// (a BigQuery table), returning "" when the reference lacks a dataset or table id.
func bigQueryAuthorizedViewFullName(ctx ExtractContext, ref *bigQueryTableRef) string {
	project := firstNonEmpty(strings.TrimSpace(ref.ProjectID), ctx.ProjectID)
	dataset := strings.TrimSpace(ref.DatasetID)
	table := strings.TrimSpace(ref.TableID)
	if project == "" || dataset == "" || table == "" {
		return ""
	}
	return fmt.Sprintf("%sprojects/%s/datasets/%s/tables/%s", bigQueryResourceNamePrefix, project, dataset, table)
}

// bigQueryRoutineFullName builds the CAI full resource name for an authorized
// routine, returning "" when the reference lacks a dataset or routine id.
func bigQueryRoutineFullName(ctx ExtractContext, ref *bigQueryRoutineRef) string {
	project := firstNonEmpty(strings.TrimSpace(ref.ProjectID), ctx.ProjectID)
	dataset := strings.TrimSpace(ref.DatasetID)
	routine := strings.TrimSpace(ref.RoutineID)
	if project == "" || dataset == "" || routine == "" {
		return ""
	}
	return fmt.Sprintf("%sprojects/%s/datasets/%s/routines/%s", bigQueryResourceNamePrefix, project, dataset, routine)
}

// bigQueryAuthorizedDatasetFullName builds the CAI full resource name for an
// authorized dataset, returning "" when the target dataset id is absent.
func bigQueryAuthorizedDatasetFullName(ctx ExtractContext, ref *bigQueryDatasetAccessTarget) string {
	project := firstNonEmpty(strings.TrimSpace(ref.Dataset.ProjectID), ctx.ProjectID)
	dataset := strings.TrimSpace(ref.Dataset.DatasetID)
	if project == "" || dataset == "" {
		return ""
	}
	return fmt.Sprintf("%sprojects/%s/datasets/%s", bigQueryResourceNamePrefix, project, dataset)
}

// bigQueryDatasetAttributes assembles the bounded attribute map. Empty or absent
// fields are omitted rather than written as zero values so a partial CAI page
// does not fabricate a "0ms expiration" or an empty ACL.
func bigQueryDatasetAttributes(data bigQueryDatasetData) map[string]any {
	attrs := map[string]any{}
	if v := strings.TrimSpace(data.Location); v != "" {
		attrs["location"] = v
	}
	if v, ok := parseInt64String(data.DefaultTableExpirationMs); ok {
		attrs["default_table_expiration_ms"] = v
	}
	if v, ok := parseInt64String(data.DefaultPartitionExpirationMs); ok {
		attrs["default_partition_expiration_ms"] = v
	}
	if v, ok := epochMillisToRFC3339(data.CreationTime); ok {
		attrs["creation_time"] = v
	}
	if v, ok := epochMillisToRFC3339(data.LastModifiedTime); ok {
		attrs["last_modified_time"] = v
	}
	addDatasetAccessSummary(attrs, data.Access)
	return attrs
}

// addDatasetAccessSummary writes the redaction-safe ACL summary: the total entry
// count, the bounded distinct role set, and the bounded distinct principal
// classes. It never writes a raw member identity — only the bounded class of
// each principal. The class comes from datasetAccessMemberClass and is one of the
// MemberClass enum values (user, group, serviceAccount, domain, principal,
// principalSet, public, authenticated, unknown) plus special (project-scoped
// special groups) and view (authorized-resource ACL entries). The typed-depth
// extractor seam carries no redaction key, so per-member fingerprints are
// intentionally out of scope here; the bounded role/class summary is sufficient
// for monitoring and drift while remaining leak-proof.
func addDatasetAccessSummary(attrs map[string]any, access []bigQueryDatasetAccessEntry) {
	if len(access) == 0 {
		return
	}
	attrs["access_entry_count"] = len(access)
	roles := newStringSet()
	classes := newStringSet()
	for _, entry := range access {
		if role := strings.TrimSpace(entry.Role); role != "" {
			roles.add(role)
		}
		classes.add(datasetAccessMemberClass(entry))
	}
	if sorted := roles.sorted(); len(sorted) > 0 {
		attrs["access_roles"] = sorted
	}
	if sorted := classes.sorted(); len(sorted) > 0 {
		attrs["access_member_classes"] = sorted
	}
}

// datasetAccessMemberClass returns the bounded principal class for one dataset
// ACL entry from whichever principal field is set. iamMember carries a prefixed
// member string and is classified through the shared MemberClass helper;
// view/dataset/routine references are authorized-resource grants classified as
// "view". The specialGroup "allAuthenticatedUsers" is a broad authenticated-user
// grant and is surfaced as "authenticated" (matching MemberClass) so posture
// readback does not hide it behind the project-scoped "special" class; the
// project-scoped special groups (projectOwners/Readers/Writers) stay "special".
// A malformed entry with no recognized principal maps to "unknown".
func datasetAccessMemberClass(entry bigQueryDatasetAccessEntry) string {
	switch {
	case strings.TrimSpace(entry.UserByEmail) != "":
		return "user"
	case strings.TrimSpace(entry.GroupByEmail) != "":
		return "group"
	case strings.TrimSpace(entry.Domain) != "":
		return "domain"
	case strings.TrimSpace(entry.SpecialGroup) != "":
		return specialGroupClass(entry.SpecialGroup)
	case strings.TrimSpace(entry.IAMMember) != "":
		return MemberClass(entry.IAMMember)
	case entry.View != nil || entry.Dataset != nil || entry.Routine != nil:
		return "view"
	default:
		return unknownLabel
	}
}

// specialGroupClass maps a BigQuery dataset specialGroup to a bounded class.
// "allAuthenticatedUsers" is the broad authenticated-user grant and is surfaced
// as "authenticated"; every project-scoped special group stays "special".
func specialGroupClass(specialGroup string) string {
	if strings.TrimSpace(specialGroup) == "allAuthenticatedUsers" {
		return "authenticated"
	}
	return "special"
}

func datasetEncryptionKMSKeyName(data bigQueryDatasetData) string {
	if data.DefaultEncryptionConfig == nil {
		return ""
	}
	return strings.TrimSpace(data.DefaultEncryptionConfig.KMSKeyName)
}

func bigQueryDatasetEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}

// stringSet collects distinct non-blank strings and returns them sorted, keeping
// the bounded role and principal-class summaries deterministic across CAI pages.
type stringSet struct {
	seen map[string]struct{}
}

func newStringSet() stringSet {
	return stringSet{seen: map[string]struct{}{}}
}

func (s stringSet) add(value string) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return
	}
	s.seen[trimmed] = struct{}{}
}

func (s stringSet) sorted() []string {
	if len(s.seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(s.seen))
	for v := range s.seen {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
