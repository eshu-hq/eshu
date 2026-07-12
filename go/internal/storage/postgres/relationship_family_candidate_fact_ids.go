// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/lib/pq"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const relationshipFamilyCandidateFactIDBatchSize = 500

const columnsPerRelationshipFamilyCandidateFactIDRow = 3

var relationshipFamilyPathPattern = regexp.MustCompile(`(^|/)(dockerfile|jenkinsfile|puppetfile|berksfile)$|(^|/)docker-compose\.ya?ml$|(^|/)compose\.ya?ml$|(^|/)\.github/workflows/[^/]+\.ya?ml$|(^|/)applicationsets?/.*\.ya?ml$|(^|/)argocd/.*\.ya?ml$|(^|/)values([^/]*)\.ya?ml$|(^|/)chart\.ya?ml$|(^|/)kustomization\.ya?ml$|(^|/)(playbooks|roles|group_vars|host_vars|inventories)/|(^|/)inventory($|/)|\.(tf|tf\.json|tfvars|tfvars\.json|hcl|tpl)$`)

type relationshipFamilyCandidateFactIDRow struct {
	FactID       string
	ScopeID      string
	GenerationID string
}

func relationshipFamilyCandidateFactIDRows(envelopes []facts.Envelope) []relationshipFamilyCandidateFactIDRow {
	if len(envelopes) == 0 {
		return nil
	}

	rows := make([]relationshipFamilyCandidateFactIDRow, 0, len(envelopes))
	for _, envelope := range envelopes {
		if envelope.IsTombstone || !relationshipFamilyCandidateFact(envelope) {
			continue
		}
		rows = append(rows, relationshipFamilyCandidateFactIDRow{
			FactID:       envelope.FactID,
			ScopeID:      envelope.ScopeID,
			GenerationID: envelope.GenerationID,
		})
	}
	return rows
}

func relationshipFamilyCandidateFact(envelope facts.Envelope) bool {
	switch envelope.FactKind {
	case facts.GCPCloudRelationshipFactKind:
		return true
	case "content", "file":
	default:
		return false
	}

	payload := decodeRelationshipFamilyPayload(envelope)
	switch payload.ArtifactType {
	case "terraform", "terraform_hcl", "terraform_template_text", "terragrunt",
		"helm", "argocd", "dockerfile", "docker_compose", "github_actions_workflow":
		return true
	default:
		if strings.HasPrefix(payload.ArtifactType, "ansible_") {
			return true
		}
	}

	path := payload.Path
	if relationshipFamilyPathPattern.MatchString(path) {
		return true
	}
	if payload.ArtifactType != "argocd" && !strings.HasSuffix(path, ".yaml") && !strings.HasSuffix(path, ".yml") {
		return false
	}
	return strings.Contains(payload.Content, "kind: application") || strings.Contains(payload.Content, "kind: applicationset")
}

func refreshRelationshipFamilyCandidateFactIDs(
	ctx context.Context,
	db ExecQueryer,
	envelopes []facts.Envelope,
) error {
	if db == nil || len(envelopes) == 0 {
		return nil
	}

	factIDs := acceptedFactIDs(envelopes)
	if len(factIDs) == 0 {
		return nil
	}
	if _, err := db.ExecContext(ctx, deleteRelationshipFamilyCandidateFactIDsSQL, pq.StringArray(factIDs)); err != nil {
		return fmt.Errorf("delete relationship family candidate fact ids: %w", err)
	}

	rows := relationshipFamilyCandidateFactIDRows(envelopes)
	if len(rows) == 0 {
		return nil
	}
	for start := 0; start < len(rows); start += relationshipFamilyCandidateFactIDBatchSize {
		end := start + relationshipFamilyCandidateFactIDBatchSize
		if end > len(rows) {
			end = len(rows)
		}
		if err := insertRelationshipFamilyCandidateFactIDBatch(ctx, db, rows[start:end]); err != nil {
			return err
		}
	}
	return nil
}

func insertRelationshipFamilyCandidateFactIDBatch(
	ctx context.Context,
	db ExecQueryer,
	rows []relationshipFamilyCandidateFactIDRow,
) error {
	if len(rows) == 0 {
		return nil
	}

	args := make([]any, 0, len(rows)*columnsPerRelationshipFamilyCandidateFactIDRow)
	var values strings.Builder
	for i, row := range rows {
		if i > 0 {
			values.WriteString(", ")
		}
		offset := i * columnsPerRelationshipFamilyCandidateFactIDRow
		fmt.Fprintf(&values, "($%d, $%d, $%d)", offset+1, offset+2, offset+3)
		args = append(args, row.FactID, row.ScopeID, row.GenerationID)
	}
	if _, err := db.ExecContext(ctx, insertRelationshipFamilyCandidateFactIDPrefix+values.String()+insertRelationshipFamilyCandidateFactIDSuffix, args...); err != nil {
		return fmt.Errorf("insert relationship family candidate fact-id batch (%d rows): %w", len(rows), err)
	}
	return nil
}

const deleteRelationshipFamilyCandidateFactIDsSQL = `
DELETE FROM relationship_family_candidate_fact_ids
WHERE fact_id = ANY($1::text[])
`

const insertRelationshipFamilyCandidateFactIDPrefix = `
INSERT INTO relationship_family_candidate_fact_ids (
    fact_id, scope_id, generation_id
) VALUES `

const insertRelationshipFamilyCandidateFactIDSuffix = `
ON CONFLICT (fact_id) DO UPDATE SET
    scope_id = EXCLUDED.scope_id,
    generation_id = EXCLUDED.generation_id
`
