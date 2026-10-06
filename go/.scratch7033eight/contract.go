package main

import (
	"encoding/json"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/query/codetopicparallel"
)

type poolKey struct {
	kind string
	term string
}

type poolRows map[poolKey]map[string]int

func indexPools(rows []codetopicparallel.ProbeRow, terms map[string]struct{}, cap int) (poolRows, error) {
	pools := make(poolRows)
	for _, row := range rows {
		if _, ok := terms[row.MatchedTerm]; !ok {
			return nil, fmt.Errorf("unexpected matched term %q", row.MatchedTerm)
		}
		if row.SourceKind != "entity" && row.SourceKind != "file" {
			return nil, fmt.Errorf("unexpected source kind %q", row.SourceKind)
		}
		key := poolKey{kind: row.SourceKind, term: row.MatchedTerm}
		if pools[key] == nil {
			pools[key] = make(map[string]int)
		}
		encoded, err := json.Marshal(row)
		if err != nil {
			return nil, fmt.Errorf("encode probe row: %w", err)
		}
		fingerprint := string(encoded)
		pools[key][fingerprint]++
		if pools[key][fingerprint] > 1 {
			return nil, fmt.Errorf("duplicate %s row for term %q", key.kind, key.term)
		}
		if len(pools[key]) > cap {
			return nil, fmt.Errorf("%s pool for term %q exceeds cap %d", key.kind, key.term, cap)
		}
	}
	return pools, nil
}

// validateConditionalPools checks the issue's uncapped parity and capped
// cardinality rules. Eligibility, path-first allocation, and page ordering
// still need independent live SQL checks on a shared snapshot.
func validateConditionalPools(baseline, candidate []codetopicparallel.ProbeRow, terms []string, cap int) error {
	if cap <= 0 {
		return fmt.Errorf("candidate cap must be positive: %d", cap)
	}
	allowed := make(map[string]struct{}, len(terms))
	for _, term := range terms {
		if term == "" {
			return fmt.Errorf("empty search term")
		}
		if _, exists := allowed[term]; exists {
			return fmt.Errorf("duplicate search term %q", term)
		}
		allowed[term] = struct{}{}
	}
	before, err := indexPools(baseline, allowed, cap)
	if err != nil {
		return fmt.Errorf("baseline: %w", err)
	}
	after, err := indexPools(candidate, allowed, cap)
	if err != nil {
		return fmt.Errorf("candidate: %w", err)
	}
	for _, term := range terms {
		for _, kind := range []string{"entity", "file"} {
			key := poolKey{kind: kind, term: term}
			beforeRows, afterRows := before[key], after[key]
			if len(beforeRows) != len(afterRows) {
				return fmt.Errorf("%s pool for term %q changed cardinality: %d to %d", kind, term, len(beforeRows), len(afterRows))
			}
			if len(beforeRows) == cap {
				continue
			}
			for row := range beforeRows {
				if afterRows[row] != 1 {
					return fmt.Errorf("uncapped %s pool for term %q changed rows", kind, term)
				}
			}
		}
	}
	return nil
}

// validateScope independently checks the grants and language promised by the
// workload. It does not establish that a row matches its search term.
func validateScope(rows []codetopicparallel.ProbeRow, allowedRepos []string, language string) error {
	allowed := make(map[string]struct{}, len(allowedRepos))
	for _, repoID := range allowedRepos {
		allowed[repoID] = struct{}{}
	}
	for _, row := range rows {
		if allowedRepos != nil {
			if row.RepoID == nil {
				return fmt.Errorf("row for term %q has no repository", row.MatchedTerm)
			}
			if _, ok := allowed[*row.RepoID]; !ok {
				return fmt.Errorf("row for term %q is outside repository scope", row.MatchedTerm)
			}
		}
		if language != "" && (row.Language == nil || *row.Language != language) {
			return fmt.Errorf("row for term %q is outside language scope", row.MatchedTerm)
		}
	}
	return nil
}

func requireReadOnlyReader(recovery bool, readOnly string) error {
	if !recovery || readOnly != "on" {
		return fmt.Errorf("reader is not a read-only standby: recovery=%t transaction_read_only=%q", recovery, readOnly)
	}
	return nil
}
