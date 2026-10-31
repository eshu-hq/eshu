// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"path"
	"strconv"
	"strings"
)

// ECSBindingDeclaration is one Terraform/Terragrunt content entity the
// workload↔ECS-service binder reads from the global declaration census.
// Metadata carries the parser row verbatim (entity_metadata): resource
// attributes, unknown_attributes, count/for_each markers, variable
// defaults, local values, terragrunt inputs, and module-call context.
type ECSBindingDeclaration struct {
	RepoID     string
	Path       string
	EntityType string
	EntityName string
	Metadata   map[string]any
}

// ECSBindingCensusResult is one bounded global declaration census read.
// Truncated reports that the census hit its row ceiling: resolution
// continues on the partial context, which can only fail closed (an
// unknown reference never binds), never invent a binding.
type ECSBindingCensusResult struct {
	Declarations []ECSBindingDeclaration
	Truncated    bool
}

// ECSBindingDeclarationCensus lists active-generation Terraform and
// Terragrunt declarations corpus-wide. The production implementation
// reads bounded, index-backed fact_records; tests substitute a fixed set.
type ECSBindingDeclarationCensus interface {
	ListECSBindingDeclarations(ctx context.Context) (ECSBindingCensusResult, error)
}

// ECSBindingInstanceLookup lists the materialized WorkloadInstance
// environments for each workload id. The production implementation is one
// index-backed graph read; tests substitute a fixed map.
type ECSBindingInstanceLookup interface {
	ListECSBindingInstanceEnvironments(ctx context.Context, workloadIDs []string) (map[string][]string, error)
}

// ecsBindingResolutionContext is the bounded static-resolution input: the
// census declarations relevant to one extraction, indexed by repository,
// directory, entity type, and name.
type ecsBindingResolutionContext struct {
	Declarations []ECSBindingDeclaration
}

// ecsBindingResolveReference resolves one raw HCL expression-text value to
// a literal string using only bounded static sources, in Terraform
// precedence order:
//
//  1. A quoted literal with no interpolation resolves to itself.
//  2. When a module call or terragrunt stack bridges into the
//     declaration's directory, only its literal arguments/inputs apply:
//     a same-directory variable default is shadowed and must not resolve
//     (real Terraform would pass the call value, not the default).
//  3. Otherwise a same-directory variable default or local resolves when
//     it is itself a quoted literal (single hop, no chaining).
//
// Everything else (interpolation, data sources, resource references,
// each/count objects, cross-directory variables without a bridging call)
// stays unknown and fails closed. There is deliberately no general HCL
// evaluator here.
func ecsBindingResolveReference(raw string, ctx ecsBindingResolutionContext, repoID, declPath string) (string, bool) {
	return ecsBindingResolveInDir(raw, ctx, repoID, path.Dir(declPath))
}

// ecsBindingResolveInDir resolves one raw expression against a known
// repository directory (the caller's dir, or the declaration's own).
func ecsBindingResolveInDir(raw string, ctx ecsBindingResolutionContext, repoID, dir string) (string, bool) {
	if literal, ok := ecsBindingLiteralString(raw); ok {
		return literal, true
	}
	trimmed := strings.TrimSpace(raw)
	var name string
	switch {
	case strings.HasPrefix(trimmed, "var."):
		name = strings.TrimSpace(strings.TrimPrefix(trimmed, "var."))
	case strings.HasPrefix(trimmed, "local."):
		name = strings.TrimSpace(strings.TrimPrefix(trimmed, "local."))
	default:
		return "", false
	}
	if name == "" || strings.ContainsAny(name, ".[()\"' \t") {
		return "", false
	}
	if bridged, found := ecsBindingBridgedValue(ctx, repoID, dir, name); found {
		return bridged, true
	}
	if strings.HasPrefix(trimmed, "local.") {
		return ecsBindingSameDirLiteral(ctx, repoID, dir, "TerraformLocal", name, "value")
	}
	return ecsBindingSameDirLiteral(ctx, repoID, dir, "TerraformVariable", name, "default")
}

// ecsBindingLiteralString reports whether raw expression text is a quoted
// string literal without interpolation.
func ecsBindingLiteralString(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if len(trimmed) < 2 || !strings.HasPrefix(trimmed, `"`) || !strings.HasSuffix(trimmed, `"`) {
		return "", false
	}
	if strings.Contains(trimmed, "${") {
		return "", false
	}
	unquoted, err := strconv.Unquote(trimmed)
	if err != nil {
		return "", false
	}
	if strings.TrimSpace(unquoted) == "" {
		return "", false
	}
	return unquoted, true
}

// ecsBindingSameDirLiteral resolves name from same-directory entities of
// the given type whose key holds a literal. Distinct values (or none)
// stay unknown.
func ecsBindingSameDirLiteral(ctx ecsBindingResolutionContext, repoID, dir, entityType, name, key string) (string, bool) {
	values := map[string]struct{}{}
	for _, decl := range ctx.Declarations {
		if decl.RepoID != repoID || decl.EntityType != entityType || path.Dir(decl.Path) != dir {
			continue
		}
		if anyToString(decl.Metadata["name"]) != name {
			continue
		}
		literal, ok := ecsBindingLiteralString(anyToString(decl.Metadata[key]))
		if !ok {
			return "", false
		}
		values[literal] = struct{}{}
	}
	if len(values) != 1 {
		return "", false
	}
	for value := range values {
		return value, true
	}
	return "", false
}

// ecsBindingBridgedValue resolves a variable from a module call or
// terragrunt stack that bridges into dir. found reports whether ANY
// bridge targets dir (shadowing same-directory defaults); the value
// resolves only when every bridging literal agrees.
func ecsBindingBridgedValue(ctx ecsBindingResolutionContext, repoID, dir, name string) (string, bool) {
	bridged := false
	values := map[string]struct{}{}
	// Terraform module calls: local-path sources resolved against the
	// call-site directory. The parser records no generic call arguments
	// today, so this leg reads metadata["arguments"] only when a future
	// parser emits it; its absence keeps the reference unknown.
	for _, decl := range ctx.Declarations {
		if decl.RepoID != repoID || decl.EntityType != "TerraformModule" {
			continue
		}
		if ecsBindingSourceTargetsDir(anyToString(decl.Metadata["source"]), path.Dir(decl.Path), dir) {
			bridged = true
			args, _ := decl.Metadata["arguments"].(map[string]any)
			literal, ok := ecsBindingLiteralString(anyToString(args[name]))
			if !ok {
				return "", true
			}
			values[literal] = struct{}{}
		}
	}
	// Terragrunt stacks: inputs apply to the called module only when the
	// stack's terraform_source resolves to the declaration directory.
	configured := map[string]bool{}
	for _, decl := range ctx.Declarations {
		if decl.RepoID != repoID || decl.EntityType != "TerragruntConfig" {
			continue
		}
		if ecsBindingSourceTargetsDir(anyToString(decl.Metadata["terraform_source"]), path.Dir(decl.Path), dir) {
			configured[path.Dir(decl.Path)] = true
		}
	}
	for _, decl := range ctx.Declarations {
		if decl.RepoID != repoID || decl.EntityType != "TerragruntInput" {
			continue
		}
		if !configured[path.Dir(decl.Path)] {
			continue
		}
		if anyToString(decl.Metadata["name"]) != name {
			continue
		}
		bridged = true
		literal, ok := ecsBindingLiteralString(anyToString(decl.Metadata["value"]))
		if !ok {
			return "", true
		}
		values[literal] = struct{}{}
	}
	if !bridged {
		return "", false
	}
	if len(values) != 1 {
		return "", false
	}
	for value := range values {
		return value, true
	}
	return "", false
}

// ecsBindingSourceTargetsDir reports whether a module source string,
// resolved against the calling file's directory, names the declaration
// directory. Only relative local paths can match; registry, remote, and
// absolute sources never bridge (fail closed).
func ecsBindingSourceTargetsDir(source, callerDir, declDir string) bool {
	trimmed := strings.TrimSpace(source)
	if trimmed == "" || path.IsAbs(trimmed) {
		return false
	}
	if strings.Contains(trimmed, "://") || strings.HasPrefix(trimmed, "git::") {
		return false
	}
	cleaned := path.Clean(path.Join(callerDir, trimmed))
	if cleaned == "." {
		cleaned = ""
	}
	target := path.Clean(declDir)
	if target == "." {
		target = ""
	}
	return cleaned != "" && cleaned == target
}

// ecsBindingRefKind names which static source a reference resolved
// through, for the row's service_anchor_reason.
type ecsBindingRefKind string

const (
	ecsBindingRefLiteral    ecsBindingRefKind = "literal"
	ecsBindingRefVariable   ecsBindingRefKind = "variable"
	ecsBindingRefLocal      ecsBindingRefKind = "local"
	ecsBindingRefTerragrunt ecsBindingRefKind = "terragrunt"
	ecsBindingRefModule     ecsBindingRefKind = "module"
	ecsBindingRefOther      ecsBindingRefKind = "other"
)

// ecsBindingReferenceKind classifies one raw reference without resolving
// it: literals first, then the source a bare var./local. ref would read.
func ecsBindingReferenceKind(raw string, ctx ecsBindingResolutionContext, repoID, declPath string) ecsBindingRefKind {
	trimmed := strings.TrimSpace(raw)
	if _, ok := ecsBindingLiteralString(trimmed); ok {
		return ecsBindingRefLiteral
	}
	dir := path.Dir(declPath)
	switch {
	case strings.HasPrefix(trimmed, "var."):
		name := strings.TrimSpace(strings.TrimPrefix(trimmed, "var."))
		if name == "" {
			return ecsBindingRefOther
		}
		if _, found := ecsBindingBridgedValue(ctx, repoID, dir, name); found {
			if ecsBindingTerragruntBridgeTargets(ctx, repoID, dir) {
				return ecsBindingRefTerragrunt
			}
			return ecsBindingRefModule
		}
		return ecsBindingRefVariable
	case strings.HasPrefix(trimmed, "local."):
		return ecsBindingRefLocal
	default:
		return ecsBindingRefOther
	}
}

// ecsBindingTerragruntBridgeTargets reports whether any terragrunt stack
// bridges into dir (as opposed to a module call doing so).
func ecsBindingTerragruntBridgeTargets(ctx ecsBindingResolutionContext, repoID, dir string) bool {
	for _, decl := range ctx.Declarations {
		if decl.RepoID != repoID || decl.EntityType != "TerragruntConfig" {
			continue
		}
		if ecsBindingSourceTargetsDir(anyToString(decl.Metadata["terraform_source"]), path.Dir(decl.Path), dir) {
			return true
		}
	}
	return false
}

// ecsBindingResolvedDeclaration is one declaration resolved once per
// extraction: resolution runs over the census, not per service, so the
// extraction is linear in declarations plus services.
type ecsBindingResolvedDeclaration struct {
	decl      ECSBindingDeclaration
	name      string
	nameOK    bool
	cluster   string
	clusterOK bool
	repeat    bool
	reason    string
}

// ecsBindingResolveDeclarations resolves every ECS service declaration in
// the census once. count/for_each declarations never produce claims: when
// their name resolves they poison that name as repeat-unresolved
// (multiplicity unknown, never bindable).
func ecsBindingResolveDeclarations(ctx ecsBindingResolutionContext) []ecsBindingResolvedDeclaration {
	resolved := make([]ecsBindingResolvedDeclaration, 0, len(ctx.Declarations))
	for _, decl := range ctx.Declarations {
		if decl.EntityType != "TerraformResource" {
			continue
		}
		if anyToString(decl.Metadata["resource_type"]) != "aws_ecs_service" {
			continue
		}
		entry := ecsBindingResolvedDeclaration{decl: decl}
		nameAttr := ecsBindingDeclarationAttr(decl, "name")
		entry.name, entry.nameOK = ecsBindingResolveReference(nameAttr, ctx, decl.RepoID, decl.Path)
		if anyToString(decl.Metadata["count"]) != "" || anyToString(decl.Metadata["for_each"]) != "" {
			entry.repeat = entry.nameOK
			resolved = append(resolved, entry)
			continue
		}
		entry.cluster, entry.clusterOK = ecsBindingResolveReference(
			ecsBindingDeclarationAttr(decl, "cluster"), ctx, decl.RepoID, decl.Path)
		switch ecsBindingReferenceKind(nameAttr, ctx, decl.RepoID, decl.Path) {
		case ecsBindingRefVariable:
			entry.reason = ecsBindingReasonVariableDefault
		case ecsBindingRefLocal:
			entry.reason = ecsBindingReasonLocal
		case ecsBindingRefTerragrunt:
			entry.reason = ecsBindingReasonTerragruntInput
		case ecsBindingRefModule:
			entry.reason = ecsBindingReasonModuleArgument
		default:
			entry.reason = ecsBindingReasonLiteral
		}
		resolved = append(resolved, entry)
	}
	return resolved
}

// ecsBindingDeclarationClaims collects the resolved declarations claiming
// the service's (name, cluster). repeat reports an attributable
// for_each/count claim; nameUnresolved reports a declaration whose name
// matches but whose cluster stays unknown.
func ecsBindingDeclarationClaims(
	resolved []ecsBindingResolvedDeclaration,
	service ecsBindingService,
) (matched []ecsBindingClaim, repeat bool, nameUnresolved bool) {
	for _, entry := range resolved {
		if entry.repeat {
			if entry.nameOK && entry.name == service.name {
				repeat = true
			}
			continue
		}
		if !entry.nameOK || entry.name != service.name {
			continue
		}
		if !entry.clusterOK {
			nameUnresolved = true
			continue
		}
		if !ecsBindingClusterMatches(entry.cluster, service.clusterARN) {
			continue
		}
		matched = append(matched, ecsBindingClaim{
			name: entry.name, clusterARN: entry.cluster,
			repoID: entry.decl.RepoID, path: entry.decl.Path, reason: entry.reason,
		})
	}
	return matched, repeat, nameUnresolved
}

// ecsBindingDeclarationAttr reads one raw attribute expression from a
// declaration's attributes map.
func ecsBindingDeclarationAttr(decl ECSBindingDeclaration, key string) string {
	attrs, _ := decl.Metadata["attributes"].(map[string]any)
	return anyToString(attrs[key])
}

// ecsBindingNameClaimedElsewhere reports whether any declaration claims
// the service name on a different cluster: the name is shared, so no
// cluster may bind it without disambiguation the reducer cannot perform.
