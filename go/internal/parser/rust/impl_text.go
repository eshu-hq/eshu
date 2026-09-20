// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rust

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// Impl/trait header and type-name text helpers. Split out of parser.go under
// the 500-line file cap; behavior is unchanged.
func rustNearestImplDetails(node *tree_sitter.Node, source []byte) rustImplBlockDetails {
	for current := node.Parent(); current != nil; current = current.Parent() {
		if current.Kind() != "impl_item" {
			continue
		}
		return rustImplDetails(current, source)
	}
	return rustImplBlockDetails{}
}

func rustNearestTraitName(node *tree_sitter.Node, source []byte) string {
	for current := node.Parent(); current != nil; current = current.Parent() {
		if current.Kind() != "trait_item" {
			continue
		}
		return strings.TrimSpace(shared.NodeText(firstNamedDescendant(current, "type_identifier"), source))
	}
	return ""
}

func rustImplDetails(node *tree_sitter.Node, source []byte) rustImplBlockDetails {
	header := strings.TrimSpace(shared.NodeText(node, source))
	if idx := strings.Index(header, "{"); idx >= 0 {
		header = header[:idx]
	}
	header = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(header), "unsafe "))
	header = strings.TrimSpace(strings.TrimPrefix(header, "impl"))
	details := rustImplBlockDetails{
		header:             header,
		kind:               "inherent_impl",
		lifetimeParameters: rustDeclaredLifetimeParameters(node, source),
		leadingGenerics:    rustLeadingGenericSegment(header),
		signatureLifetimes: rustSignatureLifetimeNames(node, source),
	}

	header = strings.TrimSpace(rustStripTypeParameters(header))
	details.header = header
	details.target = header
	if idx := strings.Index(header, " for "); idx >= 0 {
		details.kind = "trait_impl"
		details.trait = strings.TrimSpace(header[:idx])
		details.target = strings.TrimSpace(header[idx+len(" for "):])
	}
	details.target = rustTrimWhereClause(details.target)
	return details
}

func rustStripTypeParameters(text string) string {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "<") {
		return trimmed
	}
	if segment, ok := rustLeadingAngleSegment(trimmed); ok {
		return strings.TrimSpace(trimmed[len(segment):])
	}
	return trimmed
}

func rustImportAlias(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	if strings.Contains(trimmed, "{") || strings.HasSuffix(trimmed, "::*") {
		return ""
	}
	if idx := strings.LastIndex(trimmed, "::"); idx >= 0 {
		return strings.TrimSpace(trimmed[idx+2:])
	}
	return trimmed
}

func rustBaseTypeName(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	if idx := strings.Index(trimmed, "<"); idx >= 0 {
		trimmed = trimmed[:idx]
	}
	if idx := strings.LastIndex(trimmed, "::"); idx >= 0 {
		trimmed = trimmed[idx+2:]
	}
	return strings.TrimSpace(trimmed)
}
