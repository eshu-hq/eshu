// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hcl

import (
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// runtimeEvidence extracts static Terraform and Terragrunt version constraints
// from the already parsed body and the same source bytes used by Parse.
func runtimeEvidence(body *hclsyntax.Body, source []byte, terragrunt bool) shared.RuntimeEvidence {
	evidence := shared.NewRuntimeEvidence(source)
	if evidence.State == "over_cap" || body == nil {
		return evidence
	}
	if terragrunt {
		addLiteralVersion(&evidence, body.Attributes["terraform_version_constraint"], "terragrunt_version_constraint")
		return evidence
	}
	for _, block := range body.Blocks {
		if block.Type == "terraform" && len(block.Labels) == 0 {
			addLiteralVersion(&evidence, block.Body.Attributes["required_version"], "terraform_required_version")
		}
	}
	return evidence
}

func addLiteralVersion(evidence *shared.RuntimeEvidence, attribute *hclsyntax.Attribute, kind string) {
	if attribute == nil {
		return
	}
	switch attribute.Expr.(type) {
	case *hclsyntax.LiteralValueExpr, *hclsyntax.TemplateExpr:
	default:
		evidence.Rejected++
		return
	}
	value, diagnostics := attribute.Expr.Value(nil)
	if diagnostics.HasErrors() || !value.IsKnown() || value.IsNull() || !value.Type().Equals(cty.String) {
		evidence.Rejected++
		return
	}
	evidence.Add(kind, "terraform", value.AsString(), attribute.Range().Start.Line)
}
