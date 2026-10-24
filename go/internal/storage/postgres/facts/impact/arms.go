// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factimpact

import "strings"

// SuppressionScopeTrimCharactersSQL is PostgreSQL's explicit spelling of the
// Unicode White_Space set used by strings.TrimSpace. Keeping the sets equal
// prevents the SQL prefilter from making a suppression inert before the Go
// decoder can apply its matching contract.
const SuppressionScopeTrimCharactersSQL = `U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000'`

// JSTSLanguagePredicate is the parser-file language filter, byte for byte in
// the file branch WHERE and the JS/TS partial index predicate (with the
// fact. qualifier dropped there): the planner proves the partial index only
// from an identical expression.
const JSTSLanguagePredicate = `LOWER(COALESCE(fact.payload->'parsed_file_data'->>'language', fact.payload->>'language', '')) IN ('javascript', 'jsx', 'typescript', 'tsx')`

// armGroup is the kind gate an arm sits under in the retired disjunction.
type armGroup int

const (
	// groupUngated arms compare every scanned kind's payload.
	groupUngated armGroup = iota + 1
	// groupSuppressionScope arms are the normalized scope arms gated to
	// vulnerability.suppression.
	groupSuppressionScope
	// groupRepository arms are gated to RepositoryKinds; normalizedRepo
	// marks the $17 arm, gated again to vulnerability.suppression.
	groupRepository
	// groupFile arms are gated to file and the JS/TS language filter.
	groupFile
)

// arm is one disjunct of the retired predicate.
type arm struct {
	group armGroup
	// key is the top-level payload key whose presence the arm needs to
	// match, used for the foreign-key guard predicate; empty for arms on
	// fact.scope_id or the joined scope row.
	key   string
	param Param
	// suppressionOnly marks the $17 arm inside the repository group.
	suppressionOnly bool
	// resolvedScope marks the retired scope.source_key, scope.payload
	// repo_id, and scope.payload id arms. For a row the active-generation
	// join admits, scope is the row's own scope, so each of them holds
	// exactly when fact.scope_id is among the scopes ScopeResolutionSQL
	// resolves for the same list in the same snapshot; the three render as
	// one fact.scope_id = ANY(resolved scopes) arm, which needs no join.
	resolvedScope bool
	render        func(*binder) string
}

func payloadArm(group armGroup, key string, param Param) arm {
	return arm{group: group, key: key, param: param, render: func(b *binder) string {
		return "fact.payload->>'" + key + "' = ANY(" + b.list(param) + ")"
	}}
}

func normalizedScopeArm(group armGroup, key string, param Param, suppressionOnly bool) arm {
	return arm{group: group, key: "scope", param: param, suppressionOnly: suppressionOnly, render: func(b *binder) string {
		ref := b.list(param)
		return "(cardinality(" + ref + ") > 0 AND lower(btrim(fact.payload->'scope'->>'" + key + "', " +
			SuppressionScopeTrimCharactersSQL + ")) = ANY(" + ref + "))"
	}}
}

func columnArm(group armGroup, column string, param Param) arm {
	return arm{group: group, param: param, render: func(b *binder) string {
		return column + " = ANY(" + b.list(param) + ")"
	}}
}

// resolvedScopeArm stands for one retired scope-row arm; see arm.resolvedScope.
func resolvedScopeArm(group armGroup, resolved Param) arm {
	return arm{group: group, param: resolved, resolvedScope: true, render: func(b *binder) string {
		return "fact.scope_id = ANY(" + b.list(resolved) + ")"
	}}
}

// retiredArms lists every disjunct of the pre-#8880 predicate in its order.
// It is the one table the full-filter statements, the guard statement, the
// guard index predicate, and the exactness tests derive from.
func retiredArms() []arm {
	advisory := arm{group: groupUngated, key: "advisory_id", param: AdvisoryIDs, render: func(b *binder) string {
		ref := b.list(AdvisoryIDs)
		return "(cardinality(" + ref + ") > 0 AND (fact.payload->>'advisory_id' = ANY(" + ref + ")))"
	}}
	return []arm{
		payloadArm(groupUngated, "package_id", PackageIDs),
		normalizedScopeArm(groupSuppressionScope, "package_id", LowerPackageIDs, false),
		payloadArm(groupUngated, "purl", PURLs),
		normalizedScopeArm(groupSuppressionScope, "purl", LowerPURLs, false),
		payloadArm(groupUngated, "cve_id", CVEIDs),
		normalizedScopeArm(groupSuppressionScope, "cve_id", LowerCVEIDs, false),
		advisory,
		payloadArm(groupUngated, "subject_digest", SubjectDigests),
		normalizedScopeArm(groupSuppressionScope, "subject_digest", LowerSubjectDigests, false),
		payloadArm(groupUngated, "digest", SubjectDigests),
		payloadArm(groupUngated, "artifact_digest", SubjectDigests),
		payloadArm(groupUngated, "referrer_digest", SubjectDigests),
		payloadArm(groupUngated, "resolved_digest", SubjectDigests),
		payloadArm(groupUngated, "cpe", ProductCriteria),
		payloadArm(groupUngated, "criteria", ProductCriteria),
		payloadArm(groupUngated, "document_id", DocumentIDs),
		payloadArm(groupRepository, "repository_id", RepositoryIDs),
		payloadArm(groupRepository, "repo_id", RepositoryIDs),
		normalizedScopeArm(groupRepository, "repository_id", LowerRepositoryIDs, true),
		columnArm(groupRepository, "fact.scope_id", RepositoryIDs),
		payloadArm(groupRepository, "scope_id", RepositoryIDs),
		resolvedScopeArm(groupRepository, RepositoryScopeIDs), // scope.source_key
		resolvedScopeArm(groupRepository, RepositoryScopeIDs), // scope.payload->>'repo_id'
		resolvedScopeArm(groupRepository, RepositoryScopeIDs), // scope.payload->>'id'

		payloadArm(groupFile, "repository_id", FileRepositoryIDs),
		payloadArm(groupFile, "repo_id", FileRepositoryIDs),
		{group: groupFile, key: "scope", param: FileRepositoryIDs, render: func(b *binder) string {
			return "fact.payload->'scope'->>'repository_id' = ANY(" + b.list(FileRepositoryIDs) + ")"
		}},
		columnArm(groupFile, "fact.scope_id", FileRepositoryIDs),
		payloadArm(groupFile, "scope_id", FileRepositoryIDs),
		resolvedScopeArm(groupFile, FileRepositoryScopeIDs), // scope.source_key
		resolvedScopeArm(groupFile, FileRepositoryScopeIDs), // scope.payload->>'repo_id'
		resolvedScopeArm(groupFile, FileRepositoryScopeIDs), // scope.payload->>'id'

		payloadArm(groupUngated, "image_ref", ImageRefs),
		normalizedScopeArm(groupSuppressionScope, "advisory_id", LowerAdvisoryIDs, false),
	}
}

// renderedArms is retiredArms with the resolved-scope stand-ins rendered once
// per group: the three retired scope-row arms of a group all become the same
// fact.scope_id = ANY(resolved scopes) disjunct.
func renderedArms() []arm {
	seen := map[armGroup]bool{}
	var out []arm
	for _, candidate := range retiredArms() {
		if candidate.resolvedScope {
			if seen[candidate.group] {
				continue
			}
			seen[candidate.group] = true
		}
		out = append(out, candidate)
	}
	return out
}

// applies reports whether an arm can select a row of kind under the retired
// kind gates.
func (a arm) applies(kind string) bool {
	switch a.group {
	case groupUngated:
		return true
	case groupSuppressionScope:
		return kind == KindSuppression
	case groupRepository:
		if a.suppressionOnly {
			return kind == KindSuppression
		}
		for _, repositoryKind := range RepositoryKinds() {
			if repositoryKind == kind {
				return true
			}
		}
		return false
	case groupFile:
		return kind == KindFile
	default:
		return false
	}
}

// kindDisjunction renders every arm that applies to one kind, with the kind
// gates dropped (the statement already fixes the kind). It is exact for that
// kind: the retired predicate restricted to the kind is this disjunction.
func kindDisjunction(b *binder, kind string) string {
	var parts []string
	for _, candidate := range renderedArms() {
		if candidate.group == groupFile {
			continue
		}
		if candidate.applies(kind) {
			parts = append(parts, candidate.render(b))
		}
	}
	return "(\n      " + strings.Join(parts, "\n      OR ") + "\n  )"
}

// retiredDisjunction renders the full retired predicate with its kind gates,
// for the foreign-key guard statement whose rows may be of any growth kind or
// file.
func retiredDisjunction(b *binder) string {
	var parts, repository, file []string
	flushRepository := func() {
		if len(repository) > 0 {
			parts = append(parts, "(fact.fact_kind IN ("+quoteKinds(RepositoryKinds())+")\n      AND ("+strings.Join(repository, "\n        OR ")+"))")
			repository = nil
		}
	}
	flushFile := func() {
		if len(file) > 0 {
			parts = append(parts, "(fact.fact_kind = 'file'\n      AND ("+strings.Join(file, "\n        OR ")+")\n      AND "+JSTSLanguagePredicate+")")
			file = nil
		}
	}
	for _, candidate := range renderedArms() {
		rendered := candidate.render(b)
		switch {
		case candidate.group == groupRepository && candidate.suppressionOnly:
			repository = append(repository, "(fact.fact_kind = '"+KindSuppression+"' AND "+rendered+")")
		case candidate.group == groupRepository:
			repository = append(repository, rendered)
		case candidate.group == groupFile:
			flushRepository()
			file = append(file, rendered)
		case candidate.group == groupSuppressionScope:
			flushRepository()
			flushFile()
			parts = append(parts, "(fact.fact_kind = '"+KindSuppression+"' AND "+rendered+")")
		default:
			flushRepository()
			flushFile()
			parts = append(parts, rendered)
		}
	}
	flushRepository()
	flushFile()
	return "(\n      " + strings.Join(parts, "\n      OR ") + "\n  )"
}
