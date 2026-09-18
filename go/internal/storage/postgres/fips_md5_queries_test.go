// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"regexp"
	"strings"
	"testing"
)

func TestFIPSAdminMappingAndChangedSinceQueriesAvoidMD5(t *testing.T) {
	t.Parallel()

	queries := map[string]string{
		"list mapping":          listAdminIdPGroupMappingsQuery,
		"create mapping":        createAdminIdPGroupMappingQuery,
		"delete mapping":        deleteAdminIdPGroupMappingQuery,
		"changed-since counts":  changedSinceCountsQuery,
		"changed-since samples": changedSinceSamplesQuery,
	}
	for name, query := range queries {
		if strings.Contains(strings.ToLower(query), "md5(") {
			t.Errorf("%s invokes MD5, which fails on the ops-qa FIPS Postgres", name)
		}
	}
	refExpression := regexp.MustCompile(`encode\(sha256\(convert_to\([^\n]*, 'UTF8'\)\), 'hex'\)`).
		FindString(listAdminIdPGroupMappingsQuery)
	if refExpression == "" {
		t.Fatal("mapping list has no SHA-256 reference expression")
	}
	for name, query := range map[string]string{
		"create mapping": createAdminIdPGroupMappingQuery,
		"delete mapping": deleteAdminIdPGroupMappingQuery,
	} {
		if !strings.Contains(query, refExpression) {
			t.Errorf("%s does not use the same reference expression as mapping list", name)
		}
	}
}
