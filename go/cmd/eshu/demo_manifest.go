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
		Execute demoExecute `yaml:"execute"`
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
		m.Questions[i].Execute = m.Questions[i].Surface.Execute
		m.Questions[i].Question = strings.TrimSpace(m.Questions[i].Question)
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
	return demoAnswer{
		Question: q.Question,
		Answer:   summarizeDemoPayload(payload),
		Truth:    extractDemoTruth(payload),
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
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("MCP tools/call returned HTTP %d", resp.StatusCode)
	}
	var envelope struct {
		Result map[string]any `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode MCP reply: %w", err)
	}
	if envelope.Error != nil {
		return nil, fmt.Errorf("MCP tool error: %s", envelope.Error.Message)
	}
	return envelope.Result, nil
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
	resp, err := http.DefaultClient.Do(req)
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
	if meta, ok := payload["answer_metadata"].(map[string]any); ok {
		if truth, ok := meta["truth"].(map[string]any); ok && len(truth) > 0 {
			return truth
		}
	}
	if truth, ok := payload["truth"].(map[string]any); ok && len(truth) > 0 {
		return truth
	}
	return map[string]any{"truth": "not reported by this surface"}
}
