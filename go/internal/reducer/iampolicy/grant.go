// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iampolicy

import (
	"sort"
	"strings"

	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
	iamv1 "github.com/eshu-hq/eshu/sdk/go/factschema/iam/v1"
)

// IAM CloudResource resource_type tokens the target resolvers require a matched
// node to carry. They mirror the awscloud collector's emitted resource types;
// naming them here keeps both the escalation slice at the reducer root and the
// iamcan family off the collector package for four string constants.
const (
	// ResourceTypeRole is the aws_resource resource_type of an IAM role node.
	ResourceTypeRole = awsv1.ResourceTypeIAMRole
	// ResourceTypeUser is the aws_resource resource_type of an IAM user node.
	ResourceTypeUser = awsv1.ResourceTypeIAMUser
	// ResourceTypePolicy is the aws_resource resource_type of an IAM managed
	// policy node.
	ResourceTypePolicy = awsv1.ResourceTypeIAMPolicy
	// ResourceTypeGroup is the aws_resource resource_type of an IAM group node.
	ResourceTypeGroup = awsv1.ResourceTypeIAMGroup
)

// PrincipalGrant is one principal's conservatively-trusted effective grant: the
// union of actions from its trusted Allow statements (Allow, unconditioned, no
// NotAction/NotResource), the union of actions touched by any Deny statement, and
// the trusted statements themselves so target resolution can read the right
// resources. Conditioned / NotAction-bearing statements are counted as skips when
// they are the only carrier of a catalog action (see buildIAMPrincipalGrant).
type PrincipalGrant struct {
	TrustedActions map[string]struct{}
	DenyActions    map[string]struct{}
	// StatementsByAction maps a lowercase action to the decoded trusted
	// statements that granted it, so the target resolver can pull the resources
	// of the exact statement carrying a primitive's action (e.g. the iam:passrole
	// statement). Each statement carries its source FactID so StatementsCovering
	// can deduplicate a statement registered under several lookup keys, exactly as
	// the pre-typing envelope-based path did.
	StatementsByAction map[string][]Statement
}

// Statement pairs a decoded aws_iam_permission statement with its
// source FactID so the grant's StatementsByAction map can deduplicate a
// statement that matches more than one action-lookup key.
type Statement struct {
	FactID     string
	Permission iamv1.Permission
}

// Allows reports whether the trusted action set covers an action, honoring the
// two unambiguous wildcard shapes: "*" (all actions) and "service:*" (the
// action's service). Partial wildcards like "iam:Create*" are intentionally not
// expanded (conservative).
func (g PrincipalGrant) Allows(action string) bool {
	if _, ok := g.TrustedActions[action]; ok {
		return true
	}
	if _, ok := g.TrustedActions["*"]; ok {
		return true
	}
	if service, _, ok := strings.Cut(action, ":"); ok {
		if _, ok := g.TrustedActions[service+":*"]; ok {
			return true
		}
	}
	return false
}

// StatementsCovering returns the trusted statements whose action set grants the
// given carrier action, either exactly or via a "*"/"service:*" wildcard. A
// wildcard grant ("iam:*") registers its statement under the wildcard token, so a
// concrete-action lookup would miss it; this method resolves the carrier action to
// every statement that actually covers it, so the target resolver reads the right
// resources for a wildcard-granted primitive. Duplicate statements are returned at
// most once.
func (g PrincipalGrant) StatementsCovering(action string) []Statement {
	keys := []string{action, "*"}
	if service, _, ok := strings.Cut(action, ":"); ok {
		keys = append(keys, service+":*")
	}
	seen := make(map[string]struct{})
	out := make([]Statement, 0)
	for _, key := range keys {
		for _, statement := range g.StatementsByAction[key] {
			if _, dup := seen[statement.FactID]; dup && statement.FactID != "" {
				continue
			}
			if statement.FactID != "" {
				seen[statement.FactID] = struct{}{}
			}
			out = append(out, statement)
		}
	}
	return out
}

// Denied reports whether a Deny statement touches the action exactly or via a
// "*"/"service:*" wildcard. A Deny on a primitive's action removes the principal
// from that primitive entirely (conservative under-approximation).
func (g PrincipalGrant) Denied(action string) bool {
	if _, ok := g.DenyActions[action]; ok {
		return true
	}
	if _, ok := g.DenyActions["*"]; ok {
		return true
	}
	if service, _, ok := strings.Cut(action, ":"); ok {
		if _, ok := g.DenyActions[service+":*"]; ok {
			return true
		}
	}
	return false
}

// AllowStatementTouches reports whether an action set contains the target action
// exactly or via a "*"/"service:*" wildcard.
func AllowStatementTouches(actions []string, target string) bool {
	for _, action := range actions {
		if action == target || action == "*" {
			return true
		}
		if service, _, ok := strings.Cut(target, ":"); ok && action == service+":*" {
			return true
		}
	}
	return false
}

// StatementTouchesCatalog reports whether any of a statement's actions is a
// catalog action (exact, "*", or "service:*"). Used only to decide whether an
// untrusted (conditioned / NotAction) statement is worth counting as a skip.
func StatementTouchesCatalog(actions []string, catalogActions map[string]struct{}) bool {
	for _, action := range actions {
		if action == "*" {
			return true
		}
		if _, ok := catalogActions[action]; ok {
			return true
		}
		if service, _, ok := strings.Cut(action, ":"); ok && service != "" {
			wildcard := service + ":*"
			for catalogAction := range catalogActions {
				if catalogAction == wildcard {
					return true
				}
				if cs, _, ok := strings.Cut(catalogAction, ":"); ok && cs == service {
					return true
				}
			}
		}
	}
	return false
}

// PrincipalStatements groups one principal's decoded Permission statements
// with its resolved CloudResource node uid so primitive evaluation runs per
// principal. Each statement carries its FactID for the grant's dedup path; the
// grant builders read only the decoded Permission's typed fields.
type PrincipalStatements struct {
	PrincipalUID string
	Permissions  []Statement
}

// EdgeKey is the deduplication identity for a CAN_ESCALATE_TO edge: its two
// endpoint uids. The primitive set is an attribute of this identity, not part of
// it, so two primitives between the same pair converge on one idempotent edge.
type EdgeKey struct {
	PrincipalUID string
	TargetUID    string
}

// TargetStatus is the resolution outcome for an armed primitive's target.
type TargetStatus int

const (
	// TargetResolved means the target identity matched exactly one scanned node.
	TargetResolved TargetStatus = iota
	// TargetAmbiguous means the resource pattern was a wildcard or matched many
	// scanned nodes; conservatively not promoted to an edge.
	TargetAmbiguous
	// TargetUnresolved means the target ARN matched zero scanned nodes
	// (including a cross-account ARN whose account was not scanned).
	TargetUnresolved
)

// CollectTrustedResources unions the resources of the statements that carried a
// primitive's action, preserving the verbatim case-sensitive ARN patterns. It
// reads Resources from the decoded Permission statements.
func CollectTrustedResources(statements []Statement) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0)
	for _, statement := range statements {
		for _, resource := range statement.Permission.Resources {
			if _, ok := seen[resource]; ok {
				continue
			}
			seen[resource] = struct{}{}
			out = append(out, resource)
		}
	}
	sort.Strings(out)
	return out
}

// ResourceTypeOfARN classifies an IAM ARN's resource segment to the matching
// resource_type token, so target resolution can require the resolved node be the
// right IAM family. Returns "" for a non-IAM or unrecognized ARN.
func ResourceTypeOfARN(arn string) string {
	// arn:partition:iam::account:resource-type/path...
	parts := strings.SplitN(arn, ":", 6)
	if len(parts) < 6 || parts[2] != "iam" {
		return ""
	}
	resource := parts[5]
	switch {
	case strings.HasPrefix(resource, "policy/"):
		return ResourceTypePolicy
	case strings.HasPrefix(resource, "user/"):
		return ResourceTypeUser
	case strings.HasPrefix(resource, "role/"):
		return ResourceTypeRole
	case strings.HasPrefix(resource, "group/"):
		return ResourceTypeGroup
	default:
		return ""
	}
}

// GlobMatch is a small iterative wildcard matcher (no regexp compile per call) for
// IAM "*"/"?" resource patterns. "*" matches any run (including "/"), "?" matches
// one character. It avoids the catastrophic backtracking of a naive recursive
// matcher by tracking the last "*" position. The caller still requires the
// resolved node be a scanned IAM node of the expected type, so a greedy
// single-segment over-match cannot fabricate a cross-type edge.
func GlobMatch(pattern, value string) bool {
	var (
		p, v       int
		star       = -1
		starV      int
		plen, vlen = len(pattern), len(value)
	)
	for v < vlen {
		switch {
		case p < plen && (pattern[p] == value[v] || pattern[p] == '?'):
			p++
			v++
		case p < plen && pattern[p] == '*':
			star = p
			starV = v
			p++
		case star != -1:
			p = star + 1
			starV++
			v = starV
		default:
			return false
		}
	}
	for p < plen && pattern[p] == '*' {
		p++
	}
	return p == plen
}
