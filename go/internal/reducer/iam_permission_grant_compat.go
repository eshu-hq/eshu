// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "github.com/eshu-hq/eshu/go/internal/reducer/iampolicy"

// This file is the reducer root's compatibility surface for the IAM
// permission-statement, principal-grant, and target-resolution vocabulary that
// moved to [iampolicy] (issue #6061). Both the escalation slice at the root and
// the iamcan family evaluate the same decoded aws_iam_permission statements, so
// the shared shapes and matchers live below both; the root keeps its current
// spelling here.

// iamPermissionStatement is the root spelling of [iampolicy.Statement].
type iamPermissionStatement = iampolicy.Statement

// iamPrincipalGrant is the root spelling of [iampolicy.PrincipalGrant].
type iamPrincipalGrant = iampolicy.PrincipalGrant

// iamPrincipalStatements is the root spelling of
// [iampolicy.PrincipalStatements].
type iamPrincipalStatements = iampolicy.PrincipalStatements

// edgeKey is the root spelling of [iampolicy.EdgeKey].
type edgeKey = iampolicy.EdgeKey

// iamTargetStatus is the root spelling of [iampolicy.TargetStatus].
type iamTargetStatus = iampolicy.TargetStatus

const (
	// iamTargetResolved is [iampolicy.TargetResolved].
	iamTargetResolved = iampolicy.TargetResolved
	// iamTargetAmbiguous is [iampolicy.TargetAmbiguous].
	iamTargetAmbiguous = iampolicy.TargetAmbiguous
	// iamTargetUnresolved is [iampolicy.TargetUnresolved].
	iamTargetUnresolved = iampolicy.TargetUnresolved
)

const (
	// iamResourceTypeRole is [iampolicy.ResourceTypeRole].
	iamResourceTypeRole = iampolicy.ResourceTypeRole
	// iamResourceTypeUser is [iampolicy.ResourceTypeUser].
	iamResourceTypeUser = iampolicy.ResourceTypeUser
	// iamResourceTypePolicy is [iampolicy.ResourceTypePolicy].
	iamResourceTypePolicy = iampolicy.ResourceTypePolicy
	// iamResourceTypeGroup is [iampolicy.ResourceTypeGroup].
	iamResourceTypeGroup = iampolicy.ResourceTypeGroup
)

// allowStatementTouches forwards to [iampolicy.AllowStatementTouches].
func allowStatementTouches(actions []string, target string) bool {
	return iampolicy.AllowStatementTouches(actions, target)
}

// statementTouchesCatalog forwards to [iampolicy.StatementTouchesCatalog].
func statementTouchesCatalog(actions []string, catalogActions map[string]struct{}) bool {
	return iampolicy.StatementTouchesCatalog(actions, catalogActions)
}

// collectTrustedResources forwards to [iampolicy.CollectTrustedResources].
func collectTrustedResources(statements []iamPermissionStatement) []string {
	return iampolicy.CollectTrustedResources(statements)
}

// globMatch forwards to [iampolicy.GlobMatch].
func globMatch(pattern, value string) bool {
	return iampolicy.GlobMatch(pattern, value)
}

// iamResourceTypeOfARN forwards to [iampolicy.ResourceTypeOfARN].
func iamResourceTypeOfARN(arn string) string {
	return iampolicy.ResourceTypeOfARN(arn)
}
