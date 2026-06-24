package postgres

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/content"
)

type preparedRepositoryRefRow struct {
	repoID     string
	kind       string
	name       string
	headSHA    string
	isDefault  bool
	observedAt time.Time
}

func prepareRepositoryRefRows(
	repoID string,
	refs []content.RepositoryRef,
	indexedAt time.Time,
) ([]preparedRepositoryRefRow, error) {
	rowsByKey := make(map[string]preparedRepositoryRefRow, len(refs))
	for _, ref := range refs {
		kind := strings.TrimSpace(ref.Kind)
		if kind == "" {
			kind = "branch"
		}
		name := strings.TrimSpace(ref.Name)
		if name == "" {
			return nil, fmt.Errorf("repository ref name is required")
		}
		headSHA := strings.TrimSpace(ref.HeadSHA)
		if headSHA == "" {
			return nil, fmt.Errorf("repository ref head sha is required for %q", name)
		}
		observedAt := ref.ObservedAt.UTC()
		if observedAt.IsZero() {
			observedAt = indexedAt
		}
		key := kind + "\x00" + name
		rowsByKey[key] = preparedRepositoryRefRow{
			repoID:     repoID,
			kind:       kind,
			name:       name,
			headSHA:    headSHA,
			isDefault:  ref.Default,
			observedAt: observedAt,
		}
	}

	keys := make([]string, 0, len(rowsByKey))
	for key := range rowsByKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	rows := make([]preparedRepositoryRefRow, 0, len(keys))
	for _, key := range keys {
		rows = append(rows, rowsByKey[key])
	}
	return rows, nil
}

func (w ContentWriter) upsertRepositoryRefs(
	ctx context.Context,
	repoID string,
	rows []preparedRepositoryRefRow,
	indexedAt time.Time,
) error {
	if len(rows) == 0 {
		return nil
	}
	if _, err := w.db.ExecContext(ctx, deleteRepositoryRefsQuery, repoID); err != nil {
		return fmt.Errorf("delete repository_refs for %q: %w", repoID, err)
	}

	args := make([]any, 0, len(rows)*columnsPerRepositoryRef)
	var values strings.Builder
	for i, row := range rows {
		if i > 0 {
			values.WriteString(", ")
		}
		offset := i * columnsPerRepositoryRef
		fmt.Fprintf(
			&values,
			"($%d, $%d, $%d, $%d, $%d, $%d, $%d)",
			offset+1, offset+2, offset+3, offset+4, offset+5, offset+6, offset+7,
		)
		args = append(
			args,
			row.repoID,
			row.kind,
			row.name,
			row.headSHA,
			row.isDefault,
			row.observedAt,
			indexedAt,
		)
	}

	query := upsertRepositoryRefBatchPrefix + values.String() + upsertRepositoryRefBatchSuffix
	if _, err := w.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("upsert repository_refs batch (%d refs): %w", len(rows), err)
	}
	return nil
}
