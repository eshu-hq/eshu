package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// IdentityAPITokenListItem is the metadata-only view of one API token that is
// safe to return to the owning subject. It never includes token_hash.
// display_handle_hash is intentionally omitted: it is SHA-256(display_label)
// and presenting a hash as a "label" is misleading. A follow-up issue (#3703)
// will persist a real non-secret display label.
type IdentityAPITokenListItem struct {
	TokenID    string
	TokenClass string
	IssuedAt   time.Time
	ExpiresAt  time.Time
	RevokedAt  time.Time
}

// listLocalIdentityAPITokensBySubjectQuery selects metadata-only columns for
// tokens owned by a personal user (via identity_users subject_id_hash) or
// service principals owned by that user. It never selects token_hash.
// There is no issued_at upper bound — "list my own rows" must not hide a
// just-issued token under clock skew.
const listLocalIdentityAPITokensBySubjectQuery = `
SELECT
    tok.token_id,
    tok.token_class,
    tok.issued_at,
    tok.expires_at,
    tok.revoked_at
FROM identity_token_metadata tok
LEFT JOIN identity_users u
    ON tok.token_class = 'personal'
   AND u.subject_id_hash = $1
   AND tok.user_id = u.user_id
LEFT JOIN identity_service_principals sp
    ON tok.token_class = 'service_principal'
   AND sp.service_principal_id = tok.service_principal_id
   AND sp.owner_user_id IN (
       SELECT user_id FROM identity_users WHERE subject_id_hash = $1
   )
WHERE (
      (tok.token_class = 'personal' AND u.user_id IS NOT NULL)
      OR (tok.token_class = 'service_principal' AND sp.service_principal_id IS NOT NULL)
  )
ORDER BY tok.issued_at DESC
LIMIT 200
`

// ListAPITokensBySubject returns metadata-only token rows owned by the subject
// identified by subjectIDHash. The result never includes token_hash values.
func (s *IdentitySubjectStore) ListAPITokensBySubject(
	ctx context.Context,
	subjectIDHash string,
	asOf time.Time,
) ([]IdentityAPITokenListItem, error) {
	if s.db == nil {
		return nil, errors.New("identity subject store database is required")
	}
	subjectIDHash = strings.TrimSpace(subjectIDHash)
	if subjectIDHash == "" {
		return nil, errors.New("subject_id_hash is required")
	}
	if asOf.IsZero() {
		return nil, errors.New("as_of is required")
	}
	rows, err := s.db.QueryContext(ctx, listLocalIdentityAPITokensBySubjectQuery, subjectIDHash)
	if err != nil {
		return nil, fmt.Errorf("list api tokens by subject: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []IdentityAPITokenListItem
	for rows.Next() {
		var item IdentityAPITokenListItem
		var expiresAt sql.NullTime
		var revokedAt sql.NullTime
		if err := rows.Scan(
			&item.TokenID,
			&item.TokenClass,
			&item.IssuedAt,
			&expiresAt,
			&revokedAt,
		); err != nil {
			return nil, fmt.Errorf("scan api token list item: %w", err)
		}
		if expiresAt.Valid {
			item.ExpiresAt = expiresAt.Time.UTC()
		}
		if revokedAt.Valid {
			item.RevokedAt = revokedAt.Time.UTC()
		}
		item.IssuedAt = item.IssuedAt.UTC()
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list api tokens by subject: %w", err)
	}
	return items, nil
}
