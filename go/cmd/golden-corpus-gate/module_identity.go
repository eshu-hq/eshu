// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// moduleIdentitySource reads the two #8168 Module identity violations from the
// replayed graph: each (name, lang) that more than one keyed canonical Module
// carries, as "name@lang xN", and the count of canonical Modules (uid IS NULL)
// with no import_key.
type moduleIdentitySource interface {
	ModuleIdentityViolations(ctx context.Context) (duplicates []string, unkeyed int64, err error)
}

// checkModuleIdentity is the corpus-scale check for the #8168 Module identity
// (ruling 6(b)). After the replay, on the Neo4j leg, the issue's duplicate
// query must return zero rows and no canonical Module may lack an import_key.
// It is required and corpus-size independent: the module_import_key
// constraint and the writer make any violation a regression, not drift. Off
// Neo4j it records a non-required skip; the ruling scoped the invariant to the
// Neo4j checks, and NornicDB is not supported for deployment.
func checkModuleIdentity(ctx context.Context, source moduleIdentitySource, neo4jLeg bool, r *Report) {
	if !neo4jLeg {
		r.AddCheck("graph", "module_identity", true, false,
			"skipped: the #8168 Module identity invariant is a Neo4j-leg check; this leg is not Neo4j")
		return
	}
	duplicates, unkeyed, err := source.ModuleIdentityViolations(ctx)
	if err != nil {
		r.AddCheck("graph", "module_identity", false, true, fmt.Sprintf("could not run: %v", err))
		return
	}
	var problems []string
	if len(duplicates) > 0 {
		problems = append(problems, fmt.Sprintf("%d (name, lang) pairs on more than one keyed canonical Module: %s",
			len(duplicates), strings.Join(duplicates, ", ")))
	}
	if unkeyed > 0 {
		problems = append(problems, fmt.Sprintf("%d canonical Module nodes without import_key", unkeyed))
	}
	if len(problems) > 0 {
		r.AddCheck("graph", "module_identity", false, true, strings.Join(problems, "; "))
		return
	}
	r.AddCheck("graph", "module_identity", true, true,
		"0 duplicate (name, lang) keyed canonical Modules; 0 canonical Modules without import_key")
}

// moduleIdentityDuplicateLimit bounds how many duplicate keys the detail names.
const moduleIdentityDuplicateLimit = 20

// ModuleIdentityViolations implements moduleIdentitySource over the gate's
// Bolt driver with the shared storage/cypher statements.
func (b *boltGraphCounter) ModuleIdentityViolations(ctx context.Context) ([]string, int64, error) {
	dup, err := neo4j.ExecuteQuery(ctx, b.driver, sourcecypher.CanonicalModuleDuplicateKeysCypher, nil,
		neo4j.EagerResultTransformer, neo4j.ExecuteQueryWithDatabase(b.db))
	if err != nil {
		return nil, 0, fmt.Errorf("execute Module duplicate-key query: %w", err)
	}
	duplicates := make([]string, 0, len(dup.Records))
	for i, record := range dup.Records {
		if i == moduleIdentityDuplicateLimit {
			duplicates = append(duplicates, fmt.Sprintf("... %d more", len(dup.Records)-i))
			break
		}
		row := record.AsMap()
		duplicates = append(duplicates, fmt.Sprintf("%v@%v x%v", row["name"], row["lang"], row["n"]))
	}
	unkeyedResult, err := neo4j.ExecuteQuery(ctx, b.driver, sourcecypher.CanonicalModuleUnkeyedCountCypher, nil,
		neo4j.EagerResultTransformer, neo4j.ExecuteQueryWithDatabase(b.db))
	if err != nil {
		return nil, 0, fmt.Errorf("execute unkeyed Module count: %w", err)
	}
	if len(unkeyedResult.Records) != 1 {
		return nil, 0, fmt.Errorf("unkeyed Module count returned %d rows, want 1", len(unkeyedResult.Records))
	}
	value, _ := unkeyedResult.Records[0].Get("unkeyed")
	unkeyed, ok := value.(int64)
	if !ok {
		return nil, 0, fmt.Errorf("unkeyed Module count returned %v, want an integer", value)
	}
	return duplicates, unkeyed, nil
}
