// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

// supplyChainImpactCallerAuthorizationArgs separates caller visibility from
// the target's own current authority. A trusted shared or internal read may
// see every currently valid hosted boundary. Scoped reads require an exact
// tenant, workspace, subject class, policy and source-scope grant; AllScopes
// alone never supplies that grant.
func supplyChainImpactCallerAuthorizationArgs(ctx context.Context) []any {
	bound, ok := auth.AuthContextFromContext(ctx)
	if !ok || bound.Mode == auth.AuthModeShared {
		return []any{false, "", "", "", "", array.Of([]string{})}
	}
	bound = auth.NormalizeAuthContext(bound)
	return []any{
		true,
		bound.TenantID,
		bound.WorkspaceID,
		bound.SubjectClass,
		bound.PolicyRevisionHash,
		array.Of(bound.AllowedScopeIDs),
	}
}

// supplyChainImpactCallerVisibilitySQL is placed inside the protected source
// binding semi-join, before canonical ranking. The filter-scope parameter
// preserves an explicit intersection even for a trusted global caller.
func supplyChainImpactCallerVisibilitySQL(
	scoped, tenant, workspace, subjectClass, policy, allowedScopes, filterScopes string,
) string {
	return `
        AND (COALESCE(cardinality(` + filterScopes + `::text[]),0) = 0
             OR binding.source_scope_id = ANY(` + filterScopes + `::text[]))
        AND (NOT ` + scoped + `::boolean
             OR (target.tenant_id = ` + tenant + `
                 AND target.workspace_id = ` + workspace + `
                 AND target.subject_class = ` + subjectClass + `
                 AND binding.policy_revision_hash = ` + policy + `
                 AND binding.source_scope_id = ANY(` + allowedScopes + `::text[])))`
}
