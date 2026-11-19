// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/payload"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/scope"
)

const relationshipReferenceCandidateKeyBatchSize = 500

const columnsPerRelationshipReferenceCandidateKeyRow = 17

// relationshipReferenceCandidateKeysVersion is the sidecar row contract the
// keyed deferred-load arms trust per row (issue #10875). Version 0 rows
// predate the family/marker columns and keep the legacy text-search arms;
// version 1 rows carry every column below derived from the fact's own
// envelope alone (#10861 D2), so the reader recomposes the family predicate
// from these columns without detoasting the fact payload.
const relationshipReferenceCandidateKeysVersion = 1

type relationshipReferenceCandidateKeyRow struct {
	FactID                  string
	ScopeID                 string
	GenerationID            string
	SourceRepoID            string
	ReferenceKey            string
	KeysVersion             int
	FactKind                string
	ArtifactTypeNorm        string
	PathNorm                string
	ContentArgoCDKind       bool
	ContentSaltGitfs        bool
	FluxGitNonEmpty         bool
	UMKindApplication       bool
	UMArtifactArgoCD        bool
	UMArgoCDApplications    bool
	UMArgoCDApplicationsets bool
	UMFluxGitRepositories   bool
}

func relationshipReferenceCandidateKeyRows(envelopes []facts.Envelope) []relationshipReferenceCandidateKeyRow {
	if len(envelopes) == 0 {
		return nil
	}

	rows := make([]relationshipReferenceCandidateKeyRow, 0, len(envelopes))
	for _, envelope := range envelopes {
		row, ok, err := deriveRelationshipReferenceCandidateKeyRow(envelope)
		if err != nil || !ok {
			continue
		}
		rows = append(rows, row)
	}
	return rows
}

// deriveRelationshipReferenceCandidateKeyRow derives one version-1 sidecar
// row from a single fact envelope. It reports ok=false for tombstones and
// ineligible kinds (no row, legacy behavior), and returns the MarshalPayload
// error instead of silently skipping: the fact insert marshals the same
// payload, so an accepted envelope cannot fail here, and a failure must
// surface rather than leave the fact without its key row (#10861 D2).
//
// Every stored value derives from the envelope alone: the payload map, the
// fact kind, and the scope-derived source repo id. Catalog aliases, repo_id
// values, and the unconditional marker list stay query-time inputs, so a
// catalog change never needs a rebuild.
func deriveRelationshipReferenceCandidateKeyRow(envelope facts.Envelope) (relationshipReferenceCandidateKeyRow, bool, error) {
	if envelope.IsTombstone || !relationshipReferenceFactKind(envelope.FactKind) {
		return relationshipReferenceCandidateKeyRow{}, false, nil
	}
	payloadJSON, err := payloadstore.MarshalPayload(envelope.Payload)
	if err != nil {
		return relationshipReferenceCandidateKeyRow{}, false, fmt.Errorf("marshal relationship reference payload for fact %s: %w", envelope.FactID, err)
	}
	payload := envelope.Payload
	content := relationshipReferenceContentText(payload)
	row := relationshipReferenceCandidateKeyRow{
		FactID:                  envelope.FactID,
		ScopeID:                 envelope.ScopeID,
		GenerationID:            envelope.GenerationID,
		SourceRepoID:            relationshipReferenceSourceRepoID(envelope),
		ReferenceKey:            relationships.CatalogReferenceTokenStream(string(payloadJSON)),
		KeysVersion:             relationshipReferenceCandidateKeysVersion,
		FactKind:                envelope.FactKind,
		ArtifactTypeNorm:        strings.ToLower(relationshipReferencePayloadText(payload, "artifact_type")),
		PathNorm:                strings.ToLower(relationshipReferencePathText(payload)),
		ContentArgoCDKind:       strings.Contains(strings.ToLower(content), "kind: application"),
		ContentSaltGitfs:        relationshipReferenceSaltGitfsMatch(content),
		FluxGitNonEmpty:         relationshipReferenceFluxGitNonEmpty(payload),
		UMKindApplication:       false,
		UMArtifactArgoCD:        false,
		UMArgoCDApplications:    false,
		UMArgoCDApplicationsets: false,
		UMFluxGitRepositories:   false,
	}
	scanRelationshipReferenceMarkers(payload, &row)
	return row, true, nil
}

// relationshipReferenceContentText mirrors the SQL COALESCE over the content
// columns: the first present non-null content/content_body value. A
// non-string value is never content the SQL marker arms could match (they
// read ->> text, which renders scalars without the marker quotes), so only
// strings qualify.
func relationshipReferenceContentText(payload map[string]any) string {
	for _, key := range []string{"content", "content_body"} {
		if value, ok := payload[key].(string); ok {
			return value
		}
	}
	return ""
}

// relationshipReferencePayloadText mirrors payload->> for one key: missing
// or null is empty, strings pass through, and other scalars render as the
// SQL text extraction would.
func relationshipReferencePayloadText(payload map[string]any, key string) string {
	value, ok := payload[key]
	if !ok || value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}

// relationshipReferencePathText mirrors the DeferredRelationshipFamilyPathSQL
// COALESCE order: the first present non-null of relative_path,
// content_path, file_path, path.
func relationshipReferencePathText(payload map[string]any) string {
	for _, key := range []string{"relative_path", "content_path", "file_path", "path"} {
		if _, ok := payload[key]; !ok {
			continue
		}
		if payload[key] == nil {
			continue
		}
		return relationshipReferencePayloadText(payload, key)
	}
	return ""
}

// relationshipReferenceSaltGitfsLine matches a line-anchored 'gitfs_remotes:'
// entry: the Go form of the SQL conjunction (content LIKE
// '%gitfs_remotes:%' AND content ~ E'(^|\n)gitfs_remotes[[:space:]]*:').
// The Contains check is the LIKE arm, the anchored regex the ~ arm; a space
// before the colon passes the regex but fails LIKE, so the conjunction is
// false there exactly as in SQL.
var relationshipReferenceSaltGitfsLine = regexp.MustCompile(`(?m)^gitfs_remotes[[:space:]]*:`)

func relationshipReferenceSaltGitfsMatch(content string) bool {
	return strings.Contains(content, "gitfs_remotes:") &&
		relationshipReferenceSaltGitfsLine.MatchString(content)
}

// relationshipReferenceFluxGitNonEmpty mirrors the SQL jsonb_typeof array
// plus non-empty length guard: only a non-empty array counts, never the
// empty array the parser serializes into every YAML file.
func relationshipReferenceFluxGitNonEmpty(payload map[string]any) bool {
	parsed, ok := payload["parsed_file_data"].(map[string]any)
	if !ok {
		return false
	}
	repos, ok := parsed["flux_git_repositories"].([]any)
	return ok && len(repos) > 0
}

// scanRelationshipReferenceMarkers walks the fact's own payload once and
// sets the five unconditional-marker bits to the semantic equivalent of the
// jsonb-print substrings the $1 LIKE arm matches: any string value
// containing the marker text, or the key-shaped equivalent (a
// case-insensitive artifact_type key with value 'argocd', a non-empty
// object-first array under the bucket keys). The key form and the substring
// form are ORed so the bit is a superset of the legacy marker match:
// over-selection is safe (the in-memory matcher re-resolves), under-selection
// would drop evidence.
func scanRelationshipReferenceMarkers(payload map[string]any, row *relationshipReferenceCandidateKeyRow) {
	var walk func(value any)
	walkValue := func(key string, value any) {
		switch key {
		case "artifact_type":
			if text, ok := value.(string); ok && strings.ToLower(text) == "argocd" {
				row.UMArtifactArgoCD = true
			}
		case "argocd_applications", "argocd_applicationsets", "flux_git_repositories":
			if items, ok := value.([]any); ok && len(items) > 0 {
				if _, ok := items[0].(map[string]any); ok {
					switch key {
					case "argocd_applications":
						row.UMArgoCDApplications = true
					case "argocd_applicationsets":
						row.UMArgoCDApplicationsets = true
					default:
						row.UMFluxGitRepositories = true
					}
				}
			}
		}
	}
	walk = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			for key, item := range typed {
				walkValue(strings.ToLower(key), item)
				walk(item)
			}
		case []any:
			for _, item := range typed {
				walk(item)
			}
		case string:
			lowered := strings.ToLower(typed)
			if strings.Contains(lowered, "kind: application") {
				row.UMKindApplication = true
			}
			if strings.Contains(lowered, `"artifact_type": "argocd"`) {
				row.UMArtifactArgoCD = true
			}
			if strings.Contains(lowered, `"argocd_applications": [{`) {
				row.UMArgoCDApplications = true
			}
			if strings.Contains(lowered, `"argocd_applicationsets": [{`) {
				row.UMArgoCDApplicationsets = true
			}
			if strings.Contains(lowered, `"flux_git_repositories": [{`) {
				row.UMFluxGitRepositories = true
			}
		}
	}
	walk(payload)
}

func relationshipReferenceFactKind(factKind string) bool {
	switch factKind {
	case "content", "file", cloud.GCPCloudRelationshipFactKind:
		return true
	default:
		return false
	}
}

func relationshipReferenceSourceRepoID(envelope facts.Envelope) string {
	if repoID, _ := envelope.Payload["repo_id"].(string); strings.TrimSpace(repoID) != "" {
		return strings.ToLower(strings.TrimSpace(repoID))
	}
	return strings.ToLower(strings.TrimSpace(scopestore.RepoIDFromScopeID(envelope.ScopeID)))
}

func refreshRelationshipReferenceCandidateKeys(
	ctx context.Context,
	database db.ExecQueryer,
	envelopes []facts.Envelope,
) error {
	if database == nil || len(envelopes) == 0 {
		return nil
	}

	factIDs := acceptedFactIDs(envelopes)
	if len(factIDs) == 0 {
		return nil
	}
	if _, err := database.ExecContext(ctx, deleteRelationshipReferenceCandidateKeysSQL, array.StringArray(factIDs)); err != nil {
		return fmt.Errorf("delete relationship reference candidate keys: %w", err)
	}

	rows, err := relationshipReferenceCandidateKeyRowsStrict(envelopes)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	for start := 0; start < len(rows); start += relationshipReferenceCandidateKeyBatchSize {
		end := start + relationshipReferenceCandidateKeyBatchSize
		if end > len(rows) {
			end = len(rows)
		}
		if err := insertRelationshipReferenceCandidateKeyBatch(ctx, database, rows[start:end]); err != nil {
			return err
		}
	}
	return nil
}

// relationshipReferenceCandidateKeyRowsStrict derives every eligible row and
// propagates the first marshal failure. The save path must not commit facts
// whose key rows silently went missing: the keyed reader trusts per-row
// keys_version, so a missing row is a wrong-answer risk, not a slow path.
func relationshipReferenceCandidateKeyRowsStrict(envelopes []facts.Envelope) ([]relationshipReferenceCandidateKeyRow, error) {
	if len(envelopes) == 0 {
		return nil, nil
	}
	rows := make([]relationshipReferenceCandidateKeyRow, 0, len(envelopes))
	for _, envelope := range envelopes {
		row, ok, err := deriveRelationshipReferenceCandidateKeyRow(envelope)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func insertRelationshipReferenceCandidateKeyBatch(
	ctx context.Context,
	database db.ExecQueryer,
	rows []relationshipReferenceCandidateKeyRow,
) error {
	if len(rows) == 0 {
		return nil
	}
	args := make([]any, 0, len(rows)*columnsPerRelationshipReferenceCandidateKeyRow)
	var values strings.Builder
	for i, row := range rows {
		if i > 0 {
			values.WriteString(", ")
		}
		offset := i * columnsPerRelationshipReferenceCandidateKeyRow
		fmt.Fprintf(
			&values,
			"($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d)",
			offset+1,
			offset+2,
			offset+3,
			offset+4,
			offset+5,
			offset+6,
			offset+7,
			offset+8,
			offset+9,
			offset+10,
			offset+11,
			offset+12,
			offset+13,
			offset+14,
			offset+15,
			offset+16,
			offset+17,
		)
		args = append(args,
			row.FactID, row.ScopeID, row.GenerationID, row.SourceRepoID, row.ReferenceKey,
			row.KeysVersion, row.FactKind, row.ArtifactTypeNorm, row.PathNorm,
			row.ContentArgoCDKind, row.ContentSaltGitfs, row.FluxGitNonEmpty,
			row.UMKindApplication, row.UMArtifactArgoCD, row.UMArgoCDApplications,
			row.UMArgoCDApplicationsets, row.UMFluxGitRepositories,
		)
	}
	if _, err := database.ExecContext(ctx, insertRelationshipReferenceCandidateKeyPrefix+values.String()+insertRelationshipReferenceCandidateKeySuffix, args...); err != nil {
		return fmt.Errorf("insert relationship reference candidate key batch (%d rows): %w", len(rows), err)
	}
	return nil
}

func acceptedFactIDs(envelopes []facts.Envelope) []string {
	ids := make([]string, 0, len(envelopes))
	seen := make(map[string]struct{}, len(envelopes))
	for _, envelope := range envelopes {
		if strings.TrimSpace(envelope.FactID) == "" {
			continue
		}
		if _, ok := seen[envelope.FactID]; ok {
			continue
		}
		seen[envelope.FactID] = struct{}{}
		ids = append(ids, envelope.FactID)
	}
	return ids
}

const deleteRelationshipReferenceCandidateKeysSQL = `
DELETE FROM relationship_reference_candidate_keys
WHERE fact_id = ANY($1::text[])
`

const insertRelationshipReferenceCandidateKeyPrefix = `
INSERT INTO relationship_reference_candidate_keys (
    fact_id, scope_id, generation_id, source_repo_id, reference_key,
    keys_version, fact_kind, artifact_type_norm, path_norm,
    content_argocd_kind, content_salt_gitfs, flux_git_nonempty,
    um_kind_application, um_artifact_argocd, um_argocd_applications,
    um_argocd_applicationsets, um_flux_git_repositories
) VALUES `

const insertRelationshipReferenceCandidateKeySuffix = `
ON CONFLICT (fact_id) DO UPDATE SET
    scope_id = EXCLUDED.scope_id,
    generation_id = EXCLUDED.generation_id,
    source_repo_id = EXCLUDED.source_repo_id,
    reference_key = EXCLUDED.reference_key,
    keys_version = EXCLUDED.keys_version,
    fact_kind = EXCLUDED.fact_kind,
    artifact_type_norm = EXCLUDED.artifact_type_norm,
    path_norm = EXCLUDED.path_norm,
    content_argocd_kind = EXCLUDED.content_argocd_kind,
    content_salt_gitfs = EXCLUDED.content_salt_gitfs,
    flux_git_nonempty = EXCLUDED.flux_git_nonempty,
    um_kind_application = EXCLUDED.um_kind_application,
    um_artifact_argocd = EXCLUDED.um_artifact_argocd,
    um_argocd_applications = EXCLUDED.um_argocd_applications,
    um_argocd_applicationsets = EXCLUDED.um_argocd_applicationsets,
    um_flux_git_repositories = EXCLUDED.um_flux_git_repositories
`
