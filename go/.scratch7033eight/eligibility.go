package main

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/codetopicparallel"
	"github.com/jackc/pgx/v5"
)

const (
	oracleMaxTerms = 16
	oracleMaxCap   = 250
	oracleMaxBytes = 16 << 20
	oracleMaxField = 16 << 10
	oracleTimeout  = 60 * time.Second
)

type oracleSamples struct {
	entities []codetopicparallel.ProbeRow
	paths    []codetopicparallel.ProbeRow
	contents []codetopicparallel.ProbeRow
}

type oracleVerified struct {
	row       codetopicparallel.ProbeRow
	pathMatch bool
}

func oracleIdentity(row codetopicparallel.ProbeRow) string {
	if row.SourceKind == "entity" && row.EntityID != nil {
		return "entity\x00" + *row.EntityID
	}
	if row.SourceKind == "file" && row.RepoID != nil && row.RelativePath != nil {
		return "file\x00" + *row.RepoID + "\x00" + *row.RelativePath
	}
	return ""
}

func oracleRowSize(row codetopicparallel.ProbeRow) (int, error) {
	size := len(row.SourceKind) + len(row.MatchedTerm)
	for _, value := range []*string{
		row.RepoID, row.RelativePath, row.EntityID,
		row.EntityName, row.EntityType, row.Language,
	} {
		if value != nil {
			if len(*value) > oracleMaxField {
				return 0, fmt.Errorf("probe field exceeds %d bytes", oracleMaxField)
			}
			size += len(*value)
		}
	}
	return size + 16, nil
}

func oracleCount(sampled, cap int) int {
	if sampled < cap {
		return sampled
	}
	return cap
}

func requireSampleMembership(label string, selected map[string]struct{}, sampled []codetopicparallel.ProbeRow) error {
	for _, row := range sampled {
		if _, ok := selected[oracleIdentity(row)]; !ok {
			return fmt.Errorf("%s omitted eligible persisted row", label)
		}
	}
	return nil
}

func validateOracleTerm(term string, selected []codetopicparallel.ProbeRow, sampled oracleSamples, verified map[string]oracleVerified, cap int) error {
	if cap <= 0 || cap > oracleMaxCap {
		return fmt.Errorf("oracle cap outside 1..%d", oracleMaxCap)
	}
	for label, pool := range map[string][]codetopicparallel.ProbeRow{
		"entity": sampled.entities, "path": sampled.paths, "content": sampled.contents,
	} {
		if len(pool) > cap+1 {
			return fmt.Errorf("%s sample exceeds cap+1", label)
		}
	}
	if len(selected) > 2*cap {
		return fmt.Errorf("selected term rows exceed two capped pools")
	}
	selectedEntities := make(map[string]struct{})
	selectedPaths := make(map[string]struct{})
	selectedContents := make(map[string]struct{})
	seen := make(map[string]struct{}, len(selected))
	for _, row := range selected {
		if row.MatchedTerm != term || (row.SourceKind != "entity" && row.SourceKind != "file") {
			return fmt.Errorf("unexpected term or source kind in selected row")
		}
		identity := oracleIdentity(row)
		if identity == "" {
			return fmt.Errorf("selected row has no persisted primary key")
		}
		if _, duplicate := seen[identity]; duplicate {
			return fmt.Errorf("duplicate selected persisted identity %q", identity)
		}
		seen[identity] = struct{}{}
		persisted, ok := verified[identity]
		if !ok {
			return fmt.Errorf("selected %s row lacks persisted raw ILIKE or scope match", row.SourceKind)
		}
		if !reflect.DeepEqual(row, persisted.row) {
			return fmt.Errorf("selected %s row has malformed normalized projection", row.SourceKind)
		}
		if row.SourceKind == "entity" {
			selectedEntities[identity] = struct{}{}
		} else if persisted.pathMatch {
			selectedPaths[identity] = struct{}{}
		} else {
			selectedContents[identity] = struct{}{}
		}
	}
	entityCount := oracleCount(len(sampled.entities), cap)
	pathCount := oracleCount(len(sampled.paths), cap)
	contentCount := oracleCount(len(sampled.contents), cap-pathCount)
	if len(selectedEntities) != entityCount || len(selectedPaths) != pathCount || len(selectedContents) != contentCount {
		return fmt.Errorf("term %q allocation: entity %d/%d, path %d/%d, content %d/%d",
			term, len(selectedEntities), entityCount, len(selectedPaths), pathCount,
			len(selectedContents), contentCount)
	}
	if len(sampled.entities) <= cap {
		if err := requireSampleMembership("entity", selectedEntities, sampled.entities); err != nil {
			return err
		}
	}
	if len(sampled.paths) <= cap {
		if err := requireSampleMembership("path", selectedPaths, sampled.paths); err != nil {
			return err
		}
	}
	if len(sampled.contents) <= cap-pathCount {
		if err := requireSampleMembership("content", selectedContents, sampled.contents); err != nil {
			return err
		}
	}
	return nil
}

func oracleWhere(filters []string) string {
	if len(filters) == 0 {
		return ""
	}
	return " AND (" + strings.Join(filters, ") AND (") + ")"
}

func oracleColumns(kind string) string {
	if kind == "entity" {
		return `e.repo_id, e.relative_path, e.entity_id, e.entity_name, e.entity_type,
			coalesce(e.language, ''), e.start_line, e.end_line`
	}
	return `f.repo_id, f.relative_path, ''::text, ''::text, ''::text,
		coalesce(f.language, ''), 1, least(greatest(coalesce(f.line_count, 1), 1), 80)`
}

func readOracleQuery(ctx context.Context, tx pgx.Tx, query string, args []any, withPath bool) ([]oracleVerified, error) {
	result, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query persisted oracle rows: %w", err)
	}
	defer result.Close()
	rows := make([]oracleVerified, 0, oracleMaxCap+1)
	bytesRead := 0
	for result.Next() {
		var item oracleVerified
		scan := []any{
			&item.row.SourceKind, &item.row.MatchedTerm, &item.row.RepoID,
			&item.row.RelativePath, &item.row.EntityID, &item.row.EntityName,
			&item.row.EntityType, &item.row.Language, &item.row.StartLine, &item.row.EndLine,
		}
		if withPath {
			scan = append(scan, &item.pathMatch)
		}
		if err := result.Scan(scan...); err != nil {
			return nil, fmt.Errorf("scan persisted oracle row: %w", err)
		}
		size, err := oracleRowSize(item.row)
		if err != nil {
			return nil, err
		}
		bytesRead += size
		if bytesRead > oracleMaxBytes {
			return nil, fmt.Errorf("persisted oracle query exceeds %d-byte bound", oracleMaxBytes)
		}
		rows = append(rows, item)
		if len(rows) > oracleMaxCap+1 {
			return nil, fmt.Errorf("persisted oracle query exceeded row bound")
		}
	}
	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("iterate persisted oracle rows: %w", err)
	}
	return rows, nil
}

func sampleOraclePool(ctx context.Context, tx pgx.Tx, term, kind string, filters []string, baseArgs []any, cap int) ([]codetopicparallel.ProbeRow, error) {
	termArg := len(baseArgs) + 1
	alias, table := "e", "content_entities"
	placeholder := fmt.Sprintf("$%d", termArg)
	pattern := "'%' || " + placeholder + " || '%'"
	predicate := "(e.entity_name ILIKE " + pattern + " OR e.source_cache ILIKE " + pattern + ")"
	if kind != "entity" {
		alias, table = "f", "content_files"
		predicate = "f.relative_path ILIKE " + pattern
		if kind == "content" {
			predicate = "f.content ILIKE " + pattern + " AND f.relative_path NOT ILIKE " + pattern
		}
	}
	query := fmt.Sprintf("SELECT '%s'::text, $%d::text, %s FROM %s %s WHERE %s%s LIMIT %d",
		map[bool]string{true: "entity", false: "file"}[kind == "entity"], termArg,
		oracleColumns(kind), table, alias, predicate, oracleWhere(filters), cap+1)
	args := append(append([]any(nil), baseArgs...), term)
	read, err := readOracleQuery(ctx, tx, query, args, false)
	if err != nil {
		return nil, err
	}
	rows := make([]codetopicparallel.ProbeRow, len(read))
	for index, item := range read {
		rows[index] = item.row
	}
	return rows, nil
}

func lookupOracleSelected(ctx context.Context, tx pgx.Tx, term string, selected []codetopicparallel.ProbeRow, filters []string, baseArgs []any) (map[string]oracleVerified, error) {
	verified := make(map[string]oracleVerified, len(selected))
	entityIDs := make([]string, 0)
	fileRepos := make([]string, 0)
	filePaths := make([]string, 0)
	for _, row := range selected {
		if row.SourceKind == "entity" && row.EntityID != nil {
			entityIDs = append(entityIDs, *row.EntityID)
		} else if row.SourceKind == "file" && row.RepoID != nil && row.RelativePath != nil {
			fileRepos = append(fileRepos, *row.RepoID)
			filePaths = append(filePaths, *row.RelativePath)
		}
	}
	termArg := len(baseArgs) + 1
	if len(entityIDs) > 0 {
		query := fmt.Sprintf(`SELECT 'entity'::text, $%d::text, %s FROM content_entities e
			WHERE e.entity_id = ANY($%d::text[])
			AND (e.entity_name ILIKE '%%' || $%d || '%%' OR e.source_cache ILIKE '%%' || $%d || '%%')%s`,
			termArg, oracleColumns("entity"), termArg+1, termArg, termArg, oracleWhere(filters))
		args := append(append([]any(nil), baseArgs...), term, entityIDs)
		read, err := readOracleQuery(ctx, tx, query, args, false)
		if err != nil {
			return nil, err
		}
		for _, item := range read {
			verified[oracleIdentity(item.row)] = item
		}
	}
	if len(fileRepos) > 0 {
		query := fmt.Sprintf(`SELECT 'file'::text, $%d::text, %s,
			(f.relative_path ILIKE '%%' || $%d || '%%')
			FROM content_files f
			WHERE (f.repo_id, f.relative_path) IN
			(SELECT selected_repo, selected_path FROM unnest($%d::text[], $%d::text[])
			 AS selected(selected_repo, selected_path))
			AND (f.relative_path ILIKE '%%' || $%d || '%%' OR f.content ILIKE '%%' || $%d || '%%')%s`,
			termArg, oracleColumns("file"), termArg, termArg+1, termArg+2,
			termArg, termArg, oracleWhere(filters))
		args := append(append([]any(nil), baseArgs...), term, fileRepos, filePaths)
		read, err := readOracleQuery(ctx, tx, query, args, true)
		if err != nil {
			return nil, err
		}
		for _, item := range read {
			verified[oracleIdentity(item.row)] = item
		}
	}
	return verified, nil
}

// verifyPersistedEligibility independently checks each selected probe row and
// each per-term allocation against bounded reads of the persisted content tables.
func verifyPersistedEligibility(ctx context.Context, tx pgx.Tx, rows []codetopicparallel.ProbeRow, workload dynamicWorkload, cap int) error {
	if cap <= 0 || cap > oracleMaxCap || len(workload.terms) == 0 || len(workload.terms) > oracleMaxTerms ||
		!slices.Equal(workload.terms, normalizedTerms(workload.terms)) {
		return fmt.Errorf("invalid oracle cap or normalized terms")
	}
	if len(rows) > 2*cap*len(workload.terms) {
		return fmt.Errorf("probe exceeds global row cap")
	}
	ctx, cancel := context.WithTimeout(ctx, oracleTimeout)
	defer cancel()
	allowed := make(map[string][]codetopicparallel.ProbeRow, len(workload.terms))
	for _, term := range workload.terms {
		if len(term) > oracleMaxField {
			return fmt.Errorf("oracle term exceeds %d bytes", oracleMaxField)
		}
		allowed[term] = nil
	}
	bytesSeen := 0
	for _, row := range rows {
		if _, ok := allowed[row.MatchedTerm]; !ok {
			return fmt.Errorf("unexpected probe term %q", row.MatchedTerm)
		}
		size, err := oracleRowSize(row)
		if err != nil {
			return err
		}
		bytesSeen += size
		if bytesSeen > oracleMaxBytes {
			return fmt.Errorf("probe exceeds %d-byte bound", oracleMaxBytes)
		}
		allowed[row.MatchedTerm] = append(allowed[row.MatchedTerm], row)
	}
	for _, term := range workload.terms {
		var sampled oracleSamples
		var err error
		for _, spec := range []struct {
			kind string
			dest *[]codetopicparallel.ProbeRow
		}{{"entity", &sampled.entities}, {"path", &sampled.paths}, {"content", &sampled.contents}} {
			*spec.dest, err = sampleOraclePool(ctx, tx, term, spec.kind, workload.filters, workload.baseArgs, cap)
			if err != nil {
				return fmt.Errorf("sample %s for %q: %w", spec.kind, term, err)
			}
			for _, row := range *spec.dest {
				size, sizeErr := oracleRowSize(row)
				if sizeErr != nil {
					return sizeErr
				}
				bytesSeen += size
				if bytesSeen > oracleMaxBytes {
					return fmt.Errorf("oracle reads exceed %d-byte bound", oracleMaxBytes)
				}
			}
		}
		verified, lookupErr := lookupOracleSelected(ctx, tx, term, allowed[term], workload.filters, workload.baseArgs)
		if lookupErr != nil {
			return fmt.Errorf("lookup selected rows for %q: %w", term, lookupErr)
		}
		if err := validateOracleTerm(term, allowed[term], sampled, verified, cap); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("persisted eligibility context: %w", err)
	}
	return nil
}
