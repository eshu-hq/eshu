// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import "strings"

// This file owns the schema half of the #7245 infra search key: the label set
// that carries the derived infra_search_key property, the overflow flag
// values, and the Neo4j-only FULLTEXT index over them. The writer clause that
// computes the key lives in internal/storage/cypher/infra/search; the query
// handler that will read it lives in internal/query.

const (
	// InfraSearchKeyProperty is the derived node property that joins the
	// twelve free-text search fields of an infra node. Writers compute it in
	// Cypher from the node's own properties.
	InfraSearchKeyProperty = "infra_search_key"
	// InfraSearchOverflowProperty is the string flag every keyed node carries:
	// InfraSearchOverflowYes when the joined key is longer than
	// InfraSearchKeyMaxChars (no key is then stored) and InfraSearchOverflowNo
	// otherwise. A node without it was never keyed. It is a string, not a
	// boolean, because a FULLTEXT index only indexes strings.
	InfraSearchOverflowProperty = "infra_search_ovf"
	// InfraSearchOverflowYes marks a node whose key overflowed.
	InfraSearchOverflowYes = "T"
	// InfraSearchOverflowNo marks a node whose key is stored.
	InfraSearchOverflowNo = "F"
	// InfraSearchKeyMaxChars bounds a stored key in Unicode code points
	// (Cypher size() on the pinned Neo4j counts code points). At four UTF-8
	// bytes per code point the bound is 32,000 bytes, below Lucene's
	// 32,766-byte term limit. The keyword analyzer makes the whole key one
	// term, and a term past the limit is not a rejected row: it fails the
	// commit, leaves the node written, and marks the index FAILED until it is
	// rebuilt (#7245). The cap is what prevents that.
	InfraSearchKeyMaxChars = 8000
	// InfraSearchKeyIndexName is the Neo4j FULLTEXT index over the key and
	// the overflow flag of every infra search label.
	InfraSearchKeyIndexName = "infra_search_key_ft"
)

// infraSearchKeyLabels is the label set whose nodes carry the search key. It
// must equal querycontract.AllInfraLabels, the set the infra search handler
// reads; TestInfraSearchKeyLabelsMatchQueryTaxonomy enforces that. graph owns
// its own copy because it imports no internal package.
var infraSearchKeyLabels = []string{
	"CloudResource",
	"K8sResource",
	"KustomizeOverlay",
	"TerraformResource",
	"TerraformStateResource",
	"TerraformModule",
	"TerraformVariable",
	"TerraformOutput",
	"TerraformDataSource",
	"TerraformProvider",
	"TerraformLocal",
	"TerraformBackend",
	"TerraformImport",
	"TerraformMovedBlock",
	"TerraformRemovedBlock",
	"TerraformCheck",
	"TerraformLockProvider",
	"TerraformBlock",
	"TerragruntConfig",
	"TerragruntDependency",
	"CloudFormationResource",
	"ArgoCDApplication",
	"ArgoCDApplicationSet",
	"CrossplaneXRD",
	"CrossplaneComposition",
	"HelmChart",
	"HelmValues",
}

var infraSearchKeyLabelSet = func() map[string]struct{} {
	set := make(map[string]struct{}, len(infraSearchKeyLabels))
	for _, label := range infraSearchKeyLabels {
		set[label] = struct{}{}
	}
	return set
}()

// InfraSearchKeyLabels returns a copy of the labels whose nodes carry
// infra_search_key, in schema order.
func InfraSearchKeyLabels() []string {
	return append([]string(nil), infraSearchKeyLabels...)
}

// IsInfraSearchKeyLabel reports whether label is one of InfraSearchKeyLabels.
func IsInfraSearchKeyLabel(label string) bool {
	_, ok := infraSearchKeyLabelSet[label]
	return ok
}

// neo4jInfraSearchKeyIndex returns the one Neo4j-only FULLTEXT index over
// infra_search_key and infra_search_ovf on every infra search label. The
// keyword analyzer keeps the whole key one term so a leading-wildcard query
// matches a substring; eventually_consistent false makes a committed write
// visible to the next read. NornicDB gets no such index: it cannot evaluate
// the writer clause that fills the key (it stores the expression text,
// measured on the pinned image), so its writers omit the clause.
func neo4jInfraSearchKeyIndex() string {
	return "CREATE FULLTEXT INDEX " + InfraSearchKeyIndexName + " IF NOT EXISTS " +
		"FOR (n:" + strings.Join(infraSearchKeyLabels, "|") + ") " +
		"ON EACH [n." + InfraSearchKeyProperty + ", n." + InfraSearchOverflowProperty + "] " +
		"OPTIONS {indexConfig: {`fulltext.analyzer`: 'keyword', `fulltext.eventually_consistent`: false}}"
}
