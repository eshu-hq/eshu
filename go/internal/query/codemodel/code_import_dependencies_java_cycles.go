// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// Java package cycles for file_import_cycles (#7349). A Java import names a
// fully qualified CLASS while a cycle node is a PACKAGE, so the lift derives
// both endpoints as packages: the source from the projector-owned
// source_package edge property, the target by trimming the imported class
// name. The shared collapse rule (buildImportCycleGraph) and the shared walk
// run byte-unchanged: exact package-name equality joins packages, files of
// one package land on one node, and a same-package edge is excluded by the
// existing destination-equals-source skip.

// IsJavaCycleLanguage reports whether a language filter selects the Java
// package-cycle read. Exported because the codequery reader binds the
// collapsed Java fetch from it.
func IsJavaCycleLanguage(language string) bool {
	return strings.ToLower(strings.TrimSpace(language)) == "java"
}

// javaTargetPackage derives the target package of one Java import from its
// fully qualified class name: a single-type import loses its class name, an
// on-demand import loses its .* suffix first. A name with no package (a
// single segment, the default package) is unplaceable and reports "", never
// a guess.
//
// A static member import (import static com.acme.Foo.BAR) trims to the
// enclosing class (com.acme.Foo), not its package: the edge carries no
// import_type, so a member import is indistinguishable from a class import
// of that name. The phantom package dangles unless a repository package is
// literally named for the class, so such a leg closes no cycle. A cycle
// closed only through a static member import is missed; that conservative
// miss is documented, not silent.
func javaTargetPackage(module string) string {
	name := strings.TrimSpace(module)
	// An on-demand import already names its package: the .* stands for the
	// class, so stripping the suffix yields the package with no further cut.
	if strings.HasSuffix(name, ".*") {
		return strings.TrimSpace(strings.TrimSuffix(name, ".*"))
	}
	name = strings.TrimSpace(name)
	if name == "" || !strings.Contains(name, ".") {
		return ""
	}
	return name[:strings.LastIndex(name, ".")]
}

// collapseJavaPackageCycleRows folds file-level Java rows to one row per
// (repository, source package, target package), the fixture-side twin of the
// production JavaPackageCycleEdgeRowsCypher aggregation. It carries the same
// semantics: the earliest positive line wins, each flag is null when any
// folded edge predates the flags and the AND of the folded edges otherwise,
// and rows no package can place are dropped and counted, never guessed onto
// a node. The production fetch and this fold must agree; their shared truth
// table is TestJavaTargetPackageDerivation, and the env-gated live test
// proves the fetch end to end.
func collapseJavaPackageCycleRows(rows []map[string]any) ([]map[string]any, int) {
	type pairKey struct {
		repoID string
		source string
		target string
	}
	type pairFold struct {
		repoName string
		line     int
		hasLine  bool
		typeOnly *bool
		deferred *bool
		inferred *bool
	}
	folded := make(map[pairKey]*pairFold)
	dropped := 0
	// optionalBool reads one flag column: a boolean stays as-is, anything
	// else (nil from a pre-flag edge) is unknown.
	optionalBool := func(value any) *bool {
		next, ok := value.(bool)
		if !ok {
			return nil
		}
		return &next
	}
	// foldFlag ANDs one more edge into a pair flag: unknown stays unknown,
	// otherwise both edges must agree the import runs at load time.
	foldFlag := func(current *bool, value any) *bool {
		next := optionalBool(value)
		if current == nil || next == nil {
			return nil
		}
		merged := *current && *next
		return &merged
	}
	for _, row := range rows {
		if !IsJavaCycleLanguage(querycontract.StringVal(row, "language")) {
			dropped++
			continue
		}
		repoID := querycontract.StringVal(row, "repo_id")
		source := strings.TrimSpace(querycontract.StringVal(row, "source_package"))
		target := javaTargetPackage(querycontract.StringVal(row, "target_module"))
		if repoID == "" || source == "" || target == "" {
			dropped++
			continue
		}
		key := pairKey{repoID: repoID, source: source, target: target}
		acc, known := folded[key]
		if !known {
			acc = &pairFold{
				repoName: querycontract.StringVal(row, "repo_name"),
				typeOnly: optionalBool(row["type_only"]),
				deferred: optionalBool(row["deferred"]),
				inferred: optionalBool(row["inferred"]),
			}
			folded[key] = acc
		} else {
			acc.typeOnly = foldFlag(acc.typeOnly, row["type_only"])
			acc.deferred = foldFlag(acc.deferred, row["deferred"])
			acc.inferred = foldFlag(acc.inferred, row["inferred"])
		}
		if line := querycontract.IntVal(row, "line_number"); line > 0 && (!acc.hasLine || line < acc.line) {
			acc.line, acc.hasLine = line, true
		}
	}

	keys := make([]pairKey, 0, len(folded))
	for key := range folded {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].repoID < keys[j].repoID ||
			(keys[i].repoID == keys[j].repoID && keys[i].source < keys[j].source) ||
			(keys[i].repoID == keys[j].repoID && keys[i].source == keys[j].source && keys[i].target < keys[j].target)
	})
	collapsed := make([]map[string]any, 0, len(folded))
	for _, key := range keys {
		acc := folded[key]
		row := map[string]any{
			"repo_id":        key.repoID,
			"repo_name":      acc.repoName,
			"language":       "java",
			"source_package": key.source,
			"target_package": key.target,
			"line_number":    acc.line,
			"type_only":      nil,
			"deferred":       nil,
			"inferred":       nil,
		}
		if acc.typeOnly != nil {
			row["type_only"] = *acc.typeOnly
		}
		if acc.deferred != nil {
			row["deferred"] = *acc.deferred
		}
		if acc.inferred != nil {
			row["inferred"] = *acc.inferred
		}
		collapsed = append(collapsed, row)
	}
	return collapsed, dropped
}

// JavaPackageCycleEdgeRowsCypher returns one row per (repository, source
// package, target package) for the Java package-cycle read (#7349). It is
// the scan-bound half of the Java lift: the flagship Java repository
// carries 34,563 file-level IMPORTS rows, above the 25,000-row internal
// ceiling, so a file-level fetch fails closed there. Collapsing in the
// statement keeps the fetched rows to the distinct package pairs, which the
// committed count gate pins below the ceiling at flagship scale.
//
// Anchor, grant, and bound shape mirror FileImportCycleEdgeRowsCypher: the
// read starts at the repository anchor, the caller's grant predicates join
// the WHERE ahead of LIMIT, and LIMIT $scan_limit still bounds the
// statement. The target package derives with the same rule as
// javaTargetPackage (a .* suffix already names the package, otherwise cut
// at the last dot, drop packageless names); the flags fold null-if-any-null
// else AND, which for
// Java (whose parser never sets a flag) is false for mapped edges and null
// for pairs that include a pre-mapping edge. One null count covers all three
// flags because the writer always persists them together: a pre-flag edge
// lacks all three, so any null means unknown. Only widely supported Cypher
// scalar functions appear, so the NornicDB leg can plan it.
func JavaPackageCycleEdgeRowsCypher(req ImportDependencyRequest) string {
	var cypher strings.Builder
	cypher.WriteString("MATCH ")
	writeRepositoryNode(&cypher, "repo", req.RepoID)
	cypher.WriteString("-[:REPO_CONTAINS]->")
	writeFileNode(&cypher, "source_file", "source_file", "")
	cypher.WriteString("-[rel:IMPORTS]->")
	writeModuleNode(&cypher, "target_module", "target_module", "")
	cypher.WriteString("\n")
	writeCypherPredicates(&cypher, append(
		[]string{
			"(source_file.language = $cycle_language OR source_file.lang = $cycle_language)",
			"rel.source_package IS NOT NULL",
			"rel.source_package <> ''",
		},
		importDependencyGrantPredicates(req.Access, "repo")...,
	))
	cypher.WriteString(`WITH repo, rel.source_package AS source_package,
     CASE WHEN right(target_module.name, 2) = '.*'
          THEN substring(target_module.name, 0, size(target_module.name) - 2)
          WHEN target_module.name CONTAINS '.'
          THEN substring(target_module.name, 0, size(target_module.name) - size(last(split(target_module.name, '.'))) - 1)
          ELSE '' END AS target_package,
     rel
WHERE target_package <> ''
WITH repo.id AS repo_id, repo.name AS repo_name, source_package, target_package,
     min(CASE WHEN rel.line_number > 0 THEN rel.line_number ELSE null END) AS line_number,
     count(*) AS pair_edges,
     count(rel.type_only) AS flag_edges
RETURN repo_id,
       repo_name,
       'java' AS language,
       source_package,
       target_package,
       line_number,
       CASE WHEN pair_edges = flag_edges THEN false ELSE null END AS type_only,
       CASE WHEN pair_edges = flag_edges THEN false ELSE null END AS deferred,
       CASE WHEN pair_edges = flag_edges THEN false ELSE null END AS inferred
ORDER BY repo_id, source_package, target_package
LIMIT $scan_limit`)
	return cypher.String()
}
