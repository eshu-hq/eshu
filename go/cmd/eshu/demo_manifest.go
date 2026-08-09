// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// demoManifestPath is the committed acceptance oracle for the demo questions.
const demoManifestPath = "specs/demo-first-answers.v1.yaml"

// Execute kinds the manifest declares. A question names the surface that
// actually answers it, which is why `eshu demo` never needs a general
// natural-language query route — there isn't one.
const (
	demoExecuteMCP  = "mcp"
	demoExecuteHTTP = "http"
)

// demoExecute is a question's callable surface: which transport, which tool or
// route, and the arguments that make the answer correct for the demo corpus.
type demoExecute struct {
	Kind      string         `yaml:"kind"`
	Ref       string         `yaml:"ref"`
	Arguments map[string]any `yaml:"arguments"`
}

// demoQuestion is one entry from the manifest.
type demoQuestion struct {
	ID       string `yaml:"id"`
	Question string `yaml:"question"`
	Surface  struct {
		// Some questions nest the callable under execute; others put it
		// directly on surface. Both shapes appear in the committed manifest,
		// so both are parsed rather than assuming one.
		Kind      string         `yaml:"kind"`
		Ref       string         `yaml:"ref"`
		Arguments map[string]any `yaml:"arguments"`
		Execute   demoExecute    `yaml:"execute"`
	} `yaml:"surface"`
	// Execute is resolved from Surface.Execute after load so callers do not
	// have to know the manifest's nesting.
	Execute        demoExecute `yaml:"-"`
	ExpectedAnswer struct {
		RequiredResponseFields []string `yaml:"required_response_fields"`
	} `yaml:"expected_answer"`
}

// demoManifest is the parsed acceptance oracle.
type demoManifest struct {
	Questions []demoQuestion `yaml:"questions"`
}

// loadDemoManifest reads and validates the committed manifest.
func loadDemoManifest(path string) (demoManifest, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- program-constructed repo-relative path
	if err != nil {
		return demoManifest{}, fmt.Errorf("read demo manifest %s: %w", path, err)
	}
	var m demoManifest
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return demoManifest{}, fmt.Errorf("parse demo manifest %s: %w", path, err)
	}
	if len(m.Questions) == 0 {
		return demoManifest{}, fmt.Errorf("demo manifest %s declares no questions", path)
	}
	for i := range m.Questions {
		q := &m.Questions[i]
		q.Execute = q.Surface.Execute
		if q.Execute.Kind == "" && (q.Surface.Kind == demoExecuteMCP || q.Surface.Kind == demoExecuteHTTP) {
			q.Execute = demoExecute{Kind: q.Surface.Kind, Ref: q.Surface.Ref, Arguments: q.Surface.Arguments}
		}
		q.Question = strings.TrimSpace(q.Question)
	}
	return m, nil
}

// executeDemoQuestion calls the surface the question declares and checks the
// reply against that question's required_response_fields.
//
// The check is the point. A tool that answers with the wrong shape has not
// answered: the demo's promise is a correlated answer, and accepting any 200
// would let "the stack is up" pass as "the demo works".
func executeDemoQuestion(ctx context.Context, apiBase, mcpBase, apiKey string, q demoQuestion) (demoAnswer, error) {
	var payload map[string]any
	var err error
	switch q.Execute.Kind {
	case demoExecuteMCP:
		payload, err = callDemoMCPTool(ctx, mcpBase, apiKey, q.Execute)
	case demoExecuteHTTP:
		payload, err = callDemoHTTPRoute(ctx, apiBase, apiKey, q.Execute)
	default:
		return demoAnswer{}, fmt.Errorf(
			"question %s declares execute kind %q, which this command cannot call; the manifest supports %q and %q",
			q.ID, q.Execute.Kind, demoExecuteMCP, demoExecuteHTTP)
	}
	if err != nil {
		return demoAnswer{}, fmt.Errorf("question %s (%s %s): %w", q.ID, q.Execute.Kind, q.Execute.Ref, err)
	}
	if missing := missingDemoFields(payload, q.ExpectedAnswer.RequiredResponseFields); len(missing) > 0 {
		return demoAnswer{}, fmt.Errorf(
			"question %s answered without required field(s) %s; the surface replied but not with the shape the manifest requires",
			q.ID, strings.Join(missing, ", "))
	}
	truth := extractDemoTruth(payload)
	if len(truth) == 0 {
		return demoAnswer{}, fmt.Errorf(
			"question %s answered without truth labels; an answer with no provenance is not evidence", q.ID)
	}
	return demoAnswer{
		Question: q.Question,
		Answer:   summarizeDemoPayload(payload),
		Truth:    truth,
	}, nil
}

// callDemoMCPTool issues the JSON-RPC tools/call the MCP server expects.
func callDemoMCPTool(ctx context.Context, mcpBase, apiKey string, ex demoExecute) (map[string]any, error) {
	args := ex.Arguments
	if args == nil {
		args = map[string]any{}
	}
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params":  map[string]any{"name": ex.Ref, "arguments": args},
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(mcpBase, "/")+"/mcp/message", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := demoHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("MCP tools/call returned HTTP %d", resp.StatusCode)
	}
	var envelope struct {
		Result struct {
			// StructuredContent is the typed tool payload. MCP wraps a tool
			// result in content[] (human text) plus structuredContent (the
			// data), so the manifest's required_response_fields live here --
			// reading result directly finds only the wrapper.
			StructuredContent map[string]any `json:"structuredContent"`
			Content           []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode MCP reply: %w", err)
	}
	if envelope.Error != nil {
		return nil, fmt.Errorf("MCP tool error: %s", envelope.Error.Message)
	}
	if sc := envelope.Result.StructuredContent; len(sc) > 0 {
		// structuredContent is Eshu's own {data, truth, error} envelope, so the
		// manifest's required_response_fields live one level in, under data.
		// Checking the envelope itself finds only data/truth/error and reports
		// every required field missing.
		if inner, ok := sc["data"].(map[string]any); ok && len(inner) > 0 {
			if truth, ok := sc["truth"].(map[string]any); ok && len(truth) > 0 {
				inner["__truth"] = truth
			}
			return inner, nil
		}
		return sc, nil
	}
	// Some tools return only text. Parse it when it is JSON rather than
	// silently reporting an empty payload as a missing-field failure.
	if len(envelope.Result.Content) > 0 {
		var fromText map[string]any
		if err := json.Unmarshal([]byte(envelope.Result.Content[0].Text), &fromText); err == nil {
			return fromText, nil
		}
	}
	return nil, fmt.Errorf("MCP tool %s returned no structured payload", ex.Ref)
}

// callDemoHTTPRoute issues the "<METHOD> <path>" the manifest declares.
func callDemoHTTPRoute(ctx context.Context, apiBase, apiKey string, ex demoExecute) (map[string]any, error) {
	method, path, found := strings.Cut(strings.TrimSpace(ex.Ref), " ")
	if !found {
		return nil, fmt.Errorf("http ref %q is not \"<METHOD> <path>\"", ex.Ref)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(apiBase, "/")+path, nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := demoHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned HTTP %d", ex.Ref, resp.StatusCode)
	}
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return payload, nil
}

// missingDemoFields returns the required top-level fields absent from payload.
func missingDemoFields(payload map[string]any, required []string) []string {
	var missing []string
	for _, f := range required {
		if f == "__truth" {
			continue
		}
		if _, ok := payload[f]; !ok {
			missing = append(missing, f)
		}
	}
	return missing
}

// summarizeDemoPayload renders a one-line human answer. It prefers a summary
// the surface already wrote over anything this command invents.
func summarizeDemoPayload(payload map[string]any) string {
	if packet, ok := payload["answer_packet"].(map[string]any); ok {
		if s, ok := packet["summary"].(string); ok && s != "" {
			return s
		}
	}
	keys := make([]string, 0, len(payload))
	for k := range payload {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return "answered with " + strings.Join(keys, ", ")
}

// extractDemoTruth lifts the surface's truth labels. An answer with no truth
// is reported as such rather than given a fabricated label.
func extractDemoTruth(payload map[string]any) map[string]any {
	if truth, ok := payload["__truth"].(map[string]any); ok && len(truth) > 0 {
		return truth
	}
	if meta, ok := payload["answer_metadata"].(map[string]any); ok {
		if truth, ok := meta["truth"].(map[string]any); ok && len(truth) > 0 {
			return truth
		}
	}
	if truth, ok := payload["truth"].(map[string]any); ok && len(truth) > 0 {
		return truth
	}
	// Return nil, not a placeholder. An earlier draft manufactured a
	// non-empty map here, which made every answer look like it carried
	// provenance and defeated any check for one. An answer without truth
	// labels must be visibly without them.
	return nil
}

// RunnableForm renders the command an operator can actually run for this
// question.
//
// The guided path is generated from this rather than hand-written beside the
// manifest: an earlier draft listed services the demo corpus does not contain
// and told the operator to pipe sentences into `eshu query`, which takes no
// natural language. That printed confidently to someone in their first five
// minutes and sent them nowhere, and nothing failed.
func (q demoQuestion) RunnableForm() string {
	switch q.Execute.Kind {
	case demoExecuteMCP:
		args, err := json.Marshal(q.Execute.Arguments)
		if err != nil || len(q.Execute.Arguments) == 0 {
			return fmt.Sprintf("eshu mcp call %s", q.Execute.Ref)
		}
		return fmt.Sprintf("eshu mcp call %s --arguments '%s'", q.Execute.Ref, string(args))
	case demoExecuteHTTP:
		method, path, found := strings.Cut(strings.TrimSpace(q.Execute.Ref), " ")
		if !found {
			return q.Execute.Ref
		}
		return fmt.Sprintf("curl -s -X %s \"$ESHU_API_BASE%s\"", method, path)
	default:
		return ""
	}
}
