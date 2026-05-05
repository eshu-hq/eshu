package query

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func TestPostgresAdminStoreReplayFailedWorkItems_UsesConsistentPlaceholderOffsets(t *testing.T) {
	t.Parallel()

	db := &recordingAdminExecQueryer{
		rows: &recordingAdminRows{},
	}
	store := &postgresAdminStore{
		db:  db,
		now: func() time.Time { return time.Unix(1700000000, 0).UTC() },
	}

	_, err := store.ReplayFailedWorkItems(context.Background(), ReplayWorkItemFilter{
		WorkItemIDs:  []string{"wi-1"},
		OperatorNote: "retry after reducer failure",
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("ReplayFailedWorkItems() error = %v, want nil", err)
	}

	if got, want := len(db.queryArgs), 3; got != want {
		t.Fatalf("len(queryArgs) = %d, want %d", got, want)
	}
	if got, want := maxPlaceholderIndex(db.query), len(db.queryArgs); got != want {
		t.Fatalf("max placeholder index = %d, want %d; query = %s", got, want, db.query)
	}
	if !strings.Contains(db.query, "work_item_id = ANY($2)") {
		t.Fatalf("query = %q, want work_item_id selector to use $2", db.query)
	}
	if !strings.Contains(db.query, "LIMIT $3") {
		t.Fatalf("query = %q, want limit selector to use $3", db.query)
	}
}

type recordingAdminExecQueryer struct {
	query     string
	queryArgs []any
	rows      pgstatus.Rows
}

func (db *recordingAdminExecQueryer) QueryContext(_ context.Context, query string, args ...any) (pgstatus.Rows, error) {
	db.query = query
	db.queryArgs = append([]any(nil), args...)
	return db.rows, nil
}

func (*recordingAdminExecQueryer) ExecContext(_ context.Context, _ string, _ ...any) (sql.Result, error) {
	return nil, fmt.Errorf("unexpected ExecContext call")
}

type recordingAdminRows struct{}

func (*recordingAdminRows) Next() bool        { return false }
func (*recordingAdminRows) Scan(...any) error { return fmt.Errorf("unexpected Scan call") }
func (*recordingAdminRows) Err() error        { return nil }
func (*recordingAdminRows) Close() error      { return nil }

func maxPlaceholderIndex(query string) int {
	matches := regexp.MustCompile(`\$(\d+)`).FindAllStringSubmatch(query, -1)
	max := 0
	for _, match := range matches {
		value, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		if value > max {
			max = value
		}
	}
	return max
}
