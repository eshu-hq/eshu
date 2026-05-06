// Package query implements the Go read-path query layer for the platform
// context graph API. It provides Neo4j graph reads, Postgres content store
// reads, and HTTP handlers that replace the former Python query package.
package query

import (
	"context"
	"fmt"
	"strings"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Neo4jReader executes read-only Cypher queries against a Neo4j database.
type Neo4jReader struct {
	driver   neo4jdriver.DriverWithContext
	database string
	tracer   trace.Tracer
}

// NewNeo4jReader constructs a read-only Neo4j query executor.
func NewNeo4jReader(driver neo4jdriver.DriverWithContext, database string) *Neo4jReader {
	return &Neo4jReader{
		driver:   driver,
		database: database,
		tracer:   otel.Tracer("eshu/go/internal/query"),
	}
}

// Run executes a read-only Cypher query and returns results as maps.
func (r *Neo4jReader) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	ctx, span := r.tracer.Start(ctx, "neo4j.query",
		trace.WithAttributes(
			attribute.String("db.system", "neo4j"),
			attribute.String("db.name", r.database),
			attribute.String("db.statement", cypher),
		),
	)
	defer span.End()

	if r.driver == nil {
		err := fmt.Errorf("neo4j driver is required")
		span.RecordError(err)
		return nil, err
	}

	session := r.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeRead,
		DatabaseName: r.database,
	})
	defer func() { _ = session.Close(ctx) }()

	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("neo4j query: %w", err)
	}

	records, err := result.Collect(ctx)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("neo4j collect: %w", err)
	}

	rows := make([]map[string]any, 0, len(records))
	for _, record := range records {
		row := make(map[string]any, len(record.Keys))
		for i, key := range record.Keys {
			row[key] = record.Values[i]
		}
		rows = append(rows, row)
	}

	return rows, nil
}

// RunSingle executes a Cypher query expecting at most one result row.
func (r *Neo4jReader) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	ctx, span := r.tracer.Start(ctx, "neo4j.query.single",
		trace.WithAttributes(
			attribute.String("db.system", "neo4j"),
			attribute.String("db.name", r.database),
			attribute.String("db.statement", cypher),
		),
	)
	defer span.End()

	rows, err := r.Run(ctx, cypher, params)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

// RelationshipTypes returns the set of relationship type names in the graph.
func (r *Neo4jReader) RelationshipTypes(ctx context.Context) (map[string]struct{}, error) {
	rows, err := r.Run(ctx, "CALL db.relationshipTypes()", nil)
	if err != nil {
		return nil, err
	}
	types := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		for _, v := range row {
			if s, ok := v.(string); ok && s != "" {
				types[s] = struct{}{}
			}
		}
	}
	return types, nil
}

// StringVal safely extracts a string from a map value.
func StringVal(row map[string]any, key string) string {
	v, ok := row[key]
	if !ok || v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Sprintf("%v", v)
	}
	return s
}

// BoolVal safely extracts a bool from a map value.
func BoolVal(row map[string]any, key string) bool {
	v, ok := row[key]
	if !ok || v == nil {
		return false
	}
	b, ok := v.(bool)
	if !ok {
		return false
	}
	return b
}

// IntVal safely extracts an int from a map value.
func IntVal(row map[string]any, key string) int {
	v, ok := row[key]
	if !ok || v == nil {
		return 0
	}
	switch n := v.(type) {
	case int64:
		return int(n)
	case int:
		return n
	case float64:
		return int(n)
	default:
		return 0
	}
}

// StringSliceVal safely extracts a []string from a map value.
func StringSliceVal(row map[string]any, key string) []string {
	v, ok := row[key]
	if !ok || v == nil {
		return nil
	}
	switch s := v.(type) {
	case []string:
		return s
	case []any:
		result := make([]string, 0, len(s))
		for _, item := range s {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}
		return result
	default:
		return nil
	}
}

// RepoRef is the canonical repository reference returned by query endpoints.
type RepoRef struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	LocalPath string `json:"local_path"`
	RemoteURL string `json:"remote_url,omitempty"`
	RepoSlug  string `json:"repo_slug,omitempty"`
	HasRemote bool   `json:"has_remote"`
}

// RepoRefFromRow converts a Neo4j result row to a RepoRef.
func RepoRefFromRow(row map[string]any) RepoRef {
	localPath := StringVal(row, "local_path")
	if localPath == "" {
		localPath = StringVal(row, "path")
	}
	name := StringVal(row, "name")
	if name == "" && localPath != "" {
		parts := strings.Split(localPath, "/")
		name = parts[len(parts)-1]
	}
	return RepoRef{
		ID:        StringVal(row, "id"),
		Name:      name,
		LocalPath: localPath,
		RemoteURL: StringVal(row, "remote_url"),
		RepoSlug:  StringVal(row, "repo_slug"),
		HasRemote: BoolVal(row, "has_remote"),
	}
}

// RepoProjection returns the standard Cypher RETURN clause for repository nodes.
func RepoProjection(alias string) string {
	return fmt.Sprintf(
		"%s.id as id, %s.name as name, %s.path as path, "+
			"coalesce(%s.local_path, %s.path) as local_path, "+
			"%s.remote_url as remote_url, "+
			"%s.repo_slug as repo_slug, "+
			"coalesce(%s.has_remote, false) as has_remote",
		alias, alias, alias, alias, alias, alias, alias, alias,
	)
}
