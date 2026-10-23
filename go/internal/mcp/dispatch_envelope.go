// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	resultshape "github.com/eshu-hq/eshu/go/internal/mcp/result/shape"
	"github.com/eshu-hq/eshu/go/internal/query"
)

type dispatchResult struct {
	Value    any
	Envelope *query.ResponseEnvelope
	IsError  bool
	ToolName string
	// ResourceOnly drops the structured copy of a dual-shape result whose two
	// copies exceed the response budget. It is never set under the structured
	// shape, which has no second copy to drop.
	ResourceOnly bool
	// Shape is the wire layout the result is rendered and budgeted in. The zero
	// value renders as resultshape.Dual.
	Shape resultshape.Mode
}

// WithResultShape sets the process default tools/call result shape. A
// connection can still choose its own with the shape query parameter. The
// option records the value after resultshape.OrDual, so a value that did not
// come from resultshape.Parse folds to resultshape.Dual instead of reaching
// the render and budget code as a third shape.
func WithResultShape(mode resultshape.Mode) ServerOption {
	return func(s *Server) {
		s.shape = resultshape.OrDual(mode)
	}
}

// processShape returns the shape set by WithResultShape, or resultshape.Dual
// when the option is absent.
func (s *Server) processShape() resultshape.Mode {
	return resultshape.OrDual(s.shape)
}

// ResultShape reports the process default tools/call result shape, which a
// connection can override with the shape query parameter. cmd/mcp-server logs
// it once at startup beside the tool surface.
func (s *Server) ResultShape() resultshape.Mode {
	return s.processShape()
}

// shapeFor returns the result shape for the request ctx serves: the
// connection's override when ctx carries one, otherwise the process default.
// A context with no override is the stdio path.
func (s *Server) shapeFor(ctx context.Context) resultshape.Mode {
	if mode, ok := resultshape.FromContext(ctx); ok {
		return resultshape.OrDual(mode)
	}
	return s.processShape()
}

// renderToolResult keeps MCP response construction shared by the byte guard
// and the transport, so the measured shape is the one sent to clients. The
// dual shape sends a text summary, the payload as an embedded resource, and
// structuredContent (omitted when ResourceOnly). The structured shape sends the
// text summary and structuredContent only.
func renderToolResult(toolName string, result *dispatchResult) mcpToolResult {
	structuredShape := resultshape.OrDual(result.Shape) == resultshape.Structured
	var structuredContent any
	if result.Envelope != nil {
		summary := mcpContent{Type: "text", Text: summarizeToolText(toolName, result.Envelope)}
		if structuredShape {
			return mcpToolResult{Content: []mcpContent{summary}, StructuredContent: result.Envelope, IsError: result.IsError}
		}
		resourceText, _ := json.Marshal(result.Envelope)
		if !result.ResourceOnly {
			structuredContent = result.Envelope
		}
		return mcpToolResult{
			Content: []mcpContent{
				summary,
				{Type: "resource", Resource: &mcpResource{
					URI: "eshu://tool-result/envelope", MimeType: query.EnvelopeMIMEType,
					Text: string(resourceText),
				}},
			},
			StructuredContent: structuredContent,
			IsError:           result.IsError,
		}
	}
	summary := mcpContent{Type: "text", Text: summarizePlainToolText(toolName, result.Value)}
	if structuredShape {
		return mcpToolResult{Content: []mcpContent{summary}, StructuredContent: result.Value, IsError: result.IsError}
	}
	resourceText, _ := json.Marshal(result.Value)
	if !result.ResourceOnly {
		structuredContent = result.Value
	}
	return mcpToolResult{
		Content: []mcpContent{
			summary,
			{Type: "resource", Resource: &mcpResource{
				URI: "eshu://tool-result/payload", MimeType: "application/json",
				Text: string(resourceText),
			}},
		},
		StructuredContent: structuredContent,
		IsError:           result.IsError,
	}
}

// mcpToolErrorResult renders a dispatch failure. A failure with structured
// detail (a timeout or cancellation) carries it as structuredContent in both
// shapes; the dual shape also repeats it as the eshu://tool-error/dispatch
// resource.
func mcpToolErrorResult(mode resultshape.Mode, err error) mcpToolResult {
	result := mcpToolResult{
		Content: []mcpContent{{Type: "text", Text: err.Error()}},
		IsError: true,
	}
	structured, ok := dispatchErrorStructuredContent(err)
	if !ok {
		return result
	}
	result.StructuredContent = structured
	if resultshape.OrDual(mode) == resultshape.Structured {
		return result
	}
	structuredJSON, _ := json.Marshal(structured)
	result.Content = append(result.Content, mcpContent{
		Type: "resource",
		Resource: &mcpResource{
			URI:      "eshu://tool-error/dispatch",
			MimeType: "application/json",
			Text:     string(structuredJSON),
		},
	})
	return result
}

// estimateResponseTokens converts a serialized byte size into a conservative
// token estimate using the repo's shared ~4-bytes-per-token heuristic. It is a
// bound, not a billing-grade tokenizer.
func estimateResponseTokens(bytes int) int {
	if bytes <= 0 {
		return 0
	}
	return (bytes + 3) / 4
}

// overBudgetResult builds the bounded canonical envelope returned in place of an
// over-budget tool response.
func overBudgetResult(toolName string, size, budget int) *dispatchResult {
	envelope := &query.ResponseEnvelope{
		Data: nil,
		Error: &query.ErrorEnvelope{
			Code:       errorCodeResponseOverBudget,
			Message:    fmt.Sprintf("MCP tool %q response of %d bytes exceeds the %d byte response budget", toolName, size, budget),
			Capability: "mcp.dispatch",
			Details: map[string]any{
				"tool":             toolName,
				"response_bytes":   size,
				"budget_bytes":     budget,
				"estimated_tokens": estimateResponseTokens(size),
				"guidance":         responseBudgetGuidance(),
			},
		},
	}
	return &dispatchResult{
		Value:    envelope,
		Envelope: envelope,
		IsError:  true,
	}
}

// responseBudgetGuidance returns a deterministic instruction teaching the agent
// how to shrink a tool response that exceeded the dispatch budget.
func responseBudgetGuidance() string {
	return "response exceeded the MCP response budget; lower limit, add repo_id/scope filters, " +
		"request a single relationship_type or direction, set a smaller token_budget where supported, " +
		"then drill in via the returned handles instead of fetching the whole result at once; " +
		"a row with source_cache_clipped=true has a clipped body, so call get_entity_content for its full text"
}

func parseCanonicalEnvelope(body []byte) (*query.ResponseEnvelope, bool) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		return nil, false
	}
	if _, ok := top["data"]; !ok {
		return nil, false
	}
	if _, ok := top["truth"]; !ok {
		return nil, false
	}
	if _, ok := top["error"]; !ok {
		return nil, false
	}

	var envelope query.ResponseEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, false
	}
	return &envelope, true
}
