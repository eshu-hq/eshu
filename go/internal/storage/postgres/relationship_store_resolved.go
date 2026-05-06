package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// GetResolvedRelationshipsForRepos returns active resolved relationships that
// touch any candidate repository as either source or target.
func (s *RelationshipStore) GetResolvedRelationshipsForRepos(
	ctx context.Context,
	repoIDs []string,
) ([]relationships.ResolvedRelationship, error) {
	repoIDs = uniqueNonEmptyStrings(repoIDs)
	if len(repoIDs) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(repoIDs))
	args := make([]any, len(repoIDs))
	for i, repoID := range repoIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = repoID
	}
	placeholderList := strings.Join(placeholders, ", ")
	sqlRows, err := s.db.QueryContext(ctx, fmt.Sprintf(listResolvedByReposSQL, placeholderList, placeholderList), args...)
	if err != nil {
		return nil, fmt.Errorf("list resolved by repos: %w", err)
	}
	defer func() { _ = sqlRows.Close() }()

	return scanResolvedRelationshipRows(sqlRows, "by repos")
}

func scanResolvedRelationshipRows(rows Rows, label string) ([]relationships.ResolvedRelationship, error) {
	var result []relationships.ResolvedRelationship
	for rows.Next() {
		var r relationships.ResolvedRelationship
		var sourceRepoID sql.NullString
		var targetRepoID sql.NullString
		var sourceEntityID sql.NullString
		var targetEntityID sql.NullString
		var relType, resSrc string
		var detailsBytes []byte
		if err := rows.Scan(
			&sourceRepoID,
			&targetRepoID,
			&sourceEntityID,
			&targetEntityID,
			&relType,
			&r.Confidence,
			&r.EvidenceCount,
			&r.Rationale,
			&resSrc,
			&detailsBytes,
		); err != nil {
			return nil, fmt.Errorf("scan resolved %s: %w", label, err)
		}
		r.SourceRepoID = nullableString(sourceRepoID)
		r.TargetRepoID = nullableString(targetRepoID)
		r.SourceEntityID = nullableString(sourceEntityID)
		r.TargetEntityID = nullableString(targetEntityID)
		r.RelationshipType = relationships.RelationshipType(relType)
		r.ResolutionSource = relationships.ResolutionSource(resSrc)
		if len(detailsBytes) > 0 {
			if err := json.Unmarshal(detailsBytes, &r.Details); err != nil {
				return nil, fmt.Errorf("unmarshal resolved %s details: %w", label, err)
			}
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
