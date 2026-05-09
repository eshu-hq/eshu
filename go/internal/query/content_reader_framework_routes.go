package query

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// ListFrameworkRoutes queries fact_records for files with framework_semantics
// route detection for a given repo_id.
func (cr *ContentReader) ListFrameworkRoutes(ctx context.Context, repoID string) ([]FrameworkRouteEvidence, error) {
	ctx, span := cr.tracer.Start(ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "list_framework_routes"),
			attribute.String("db.sql.table", "fact_records"),
		),
	)
	defer span.End()

	if repoID == "" {
		return nil, nil
	}

	rows, err := cr.db.QueryContext(ctx, `
		SELECT
			payload->>'relative_path',
			payload->'parsed_file_data'->'framework_semantics'
		FROM fact_records
		WHERE fact_kind = 'file'
		  AND payload->>'repo_id' = $1
		  AND payload->'parsed_file_data'->'framework_semantics' IS NOT NULL
		  AND jsonb_array_length(
			  COALESCE(payload->'parsed_file_data'->'framework_semantics'->'frameworks', '[]'::jsonb)
		  ) > 0
		ORDER BY payload->>'relative_path'
	`, repoID)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("list framework routes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []FrameworkRouteEvidence
	for rows.Next() {
		var relativePath string
		var rawSemantics []byte
		if err := rows.Scan(&relativePath, &rawSemantics); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan framework route: %w", err)
		}
		routes := parseFrameworkSemantics(relativePath, rawSemantics)
		results = append(results, routes...)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("framework routes iteration: %w", err)
	}
	return results, nil
}

// parseFrameworkSemantics decodes one framework_semantics JSON blob into
// FrameworkRouteEvidence entries, one per detected framework.
func parseFrameworkSemantics(relativePath string, raw []byte) []FrameworkRouteEvidence {
	if len(raw) == 0 {
		return nil
	}
	var semantics map[string]any
	if err := json.Unmarshal(raw, &semantics); err != nil {
		return nil
	}

	frameworksRaw, _ := semantics["frameworks"].([]any)
	if len(frameworksRaw) == 0 {
		return nil
	}

	results := make([]FrameworkRouteEvidence, 0, len(frameworksRaw))
	for _, fwRaw := range frameworksRaw {
		fw, _ := fwRaw.(string)
		if fw == "" {
			continue
		}
		fwData, _ := semantics[fw].(map[string]any)
		if fwData == nil {
			continue
		}
		if fw == "nextjs" {
			if route, ok := nextJSFrameworkRoute(relativePath, fwData); ok {
				results = append(results, route)
			}
			continue
		}
		routePaths := anySliceToStrings(fwData["route_paths"])
		if len(routePaths) == 0 {
			continue
		}
		results = append(results, FrameworkRouteEvidence{
			Framework:    fw,
			RelativePath: relativePath,
			RoutePaths:   routePaths,
			RouteMethods: anySliceToStrings(fwData["route_methods"]),
			RouteEntries: frameworkRouteEntries(fwData["route_entries"]),
		})
	}
	return results
}

// nextJSFrameworkRoute translates parser-owned Next.js route module metadata
// into the generic framework route shape used by service evidence.
func nextJSFrameworkRoute(relativePath string, frameworkData map[string]any) (FrameworkRouteEvidence, bool) {
	if stringValue(frameworkData["module_kind"]) != "route" {
		return FrameworkRouteEvidence{}, false
	}
	routePath := nextJSRoutePath(anySliceToStrings(frameworkData["route_segments"]))
	if routePath == "" {
		return FrameworkRouteEvidence{}, false
	}
	return FrameworkRouteEvidence{
		Framework:    "nextjs",
		RelativePath: relativePath,
		RoutePaths:   []string{routePath},
		RouteMethods: anySliceToStrings(frameworkData["route_verbs"]),
	}, true
}

// nextJSRoutePath preserves parser-emitted route segment names while removing
// Next.js route-group folders that are not part of the public URL.
func nextJSRoutePath(segments []string) string {
	pathSegments := make([]string, 0, len(segments))
	for _, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment == "" || (strings.HasPrefix(segment, "(") && strings.HasSuffix(segment, ")")) {
			continue
		}
		pathSegments = append(pathSegments, segment)
	}
	if len(pathSegments) == 0 {
		return ""
	}
	return "/" + strings.Join(pathSegments, "/")
}

func stringValue(raw any) string {
	value, _ := raw.(string)
	return strings.TrimSpace(value)
}

func anySliceToStrings(raw any) []string {
	slice, _ := raw.([]any)
	if len(slice) == 0 {
		return nil
	}
	result := make([]string, 0, len(slice))
	for _, item := range slice {
		if s, ok := item.(string); ok && s != "" {
			result = append(result, s)
		}
	}
	return result
}

// frameworkRouteEntries decodes the parser's paired route evidence while
// tolerating older facts that only carry route_paths and route_methods.
func frameworkRouteEntries(raw any) []FrameworkRouteEntryEvidence {
	slice, _ := raw.([]any)
	if len(slice) == 0 {
		return nil
	}
	result := make([]FrameworkRouteEntryEvidence, 0, len(slice))
	for _, item := range slice {
		row, _ := item.(map[string]any)
		if len(row) == 0 {
			continue
		}
		entry := FrameworkRouteEntryEvidence{
			Method: strings.TrimSpace(stringValue(row["method"])),
			Path:   strings.TrimSpace(stringValue(row["path"])),
		}
		if entry.Method == "" || entry.Path == "" {
			continue
		}
		result = append(result, entry)
	}
	return result
}
