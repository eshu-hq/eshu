package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// BrowserSessionListItem is the metadata-only view of one browser session that
// is safe to return to the owning subject. It never includes session_hash,
// csrf_token_hash, or external auth secrets.
type BrowserSessionListItem struct {
	IssuedAt          time.Time
	LastSeenAt        time.Time
	IdleExpiresAt     time.Time
	AbsoluteExpiresAt time.Time
	TenantID          string
	WorkspaceID       string
	// Current is true when this row is the caller's active session.
	Current   bool
	RevokedAt time.Time
}

// listBrowserSessionsBySubjectQuery returns metadata-only session rows for a
// given subject hash ordered by issued_at descending. It never selects
// session_hash, csrf_token_hash, or external identity secrets.
const listBrowserSessionsBySubjectQuery = `
SELECT
    issued_at,
    last_seen_at,
    idle_expires_at,
    absolute_expires_at,
    tenant_id,
    workspace_id,
    revoked_at
FROM browser_sessions
WHERE subject_id_hash = $1
  AND issued_at <= $2
ORDER BY issued_at DESC
LIMIT 200
`

// ListSessionsBySubject returns metadata-only browser session rows for the
// given subject_id_hash. The caller is responsible for marking which row is
// the current session using the session cookie hash.
func (s *BrowserSessionStore) ListSessionsBySubject(
	ctx context.Context,
	subjectIDHash string,
	asOf time.Time,
) ([]BrowserSessionListItem, error) {
	if s.db == nil {
		return nil, errors.New("browser session store database is required")
	}
	subjectIDHash = strings.TrimSpace(subjectIDHash)
	if subjectIDHash == "" {
		return nil, errors.New("subject_id_hash is required")
	}
	if asOf.IsZero() {
		return nil, errors.New("as_of is required")
	}
	rows, err := s.db.QueryContext(ctx, listBrowserSessionsBySubjectQuery, subjectIDHash, asOf.UTC())
	if err != nil {
		return nil, fmt.Errorf("list browser sessions by subject: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []BrowserSessionListItem
	for rows.Next() {
		var item BrowserSessionListItem
		var revokedAt sql.NullTime
		if err := rows.Scan(
			&item.IssuedAt,
			&item.LastSeenAt,
			&item.IdleExpiresAt,
			&item.AbsoluteExpiresAt,
			&item.TenantID,
			&item.WorkspaceID,
			&revokedAt,
		); err != nil {
			return nil, fmt.Errorf("scan browser session list item: %w", err)
		}
		if revokedAt.Valid {
			item.RevokedAt = revokedAt.Time.UTC()
		}
		item.IssuedAt = item.IssuedAt.UTC()
		item.LastSeenAt = item.LastSeenAt.UTC()
		item.IdleExpiresAt = item.IdleExpiresAt.UTC()
		item.AbsoluteExpiresAt = item.AbsoluteExpiresAt.UTC()
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list browser sessions by subject: %w", err)
	}
	return items, nil
}
