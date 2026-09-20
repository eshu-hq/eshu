// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// DeepSeekClient calls the DeepSeek chat-completions API
// (https://api-docs.deepseek.com/api/create-chat-completion) to classify
// candidate directory names. HTTPClient is injectable so tests run against
// an httptest server instead of the real network.
type DeepSeekClient struct {
	HTTPClient *http.Client
	BaseURL    string // e.g. "https://api.deepseek.com"; no trailing slash.
	APIKey     string
	Model      string // "deepseek-flash" in production.
}

// chatRequest is the subset of the DeepSeek chat-completions request body
// this gate needs.
type chatRequest struct {
	Model          string         `json:"model"`
	Messages       []chatMessage  `json:"messages"`
	Temperature    float64        `json:"temperature"`
	ResponseFormat responseFormat `json:"response_format"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type responseFormat struct {
	Type string `json:"type"`
}

// chatResponse is the subset of the DeepSeek response this gate reads.
type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// Classify sends every candidate to the model in a single batched request
// and returns the parsed findings. It returns no error and an empty report
// for an empty candidate list without making a network call.
func (c *DeepSeekClient) Classify(ctx context.Context, candidates []Candidate) (Report, error) {
	if len(candidates) == 0 {
		return Report{}, nil
	}

	reqBody := chatRequest{
		Model: c.Model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt()},
			{Role: "user", Content: buildUserPrompt(candidates)},
		},
		Temperature:    0,
		ResponseFormat: responseFormat{Type: "json_object"},
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return Report{}, fmt.Errorf("naming-glue-gate: marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return Report{}, fmt.Errorf("naming-glue-gate: building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return Report{}, fmt.Errorf("naming-glue-gate: calling DeepSeek: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Report{}, fmt.Errorf("naming-glue-gate: reading response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Report{}, fmt.Errorf("naming-glue-gate: DeepSeek returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var parsed chatResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Report{}, fmt.Errorf("naming-glue-gate: decoding DeepSeek response envelope: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return Report{}, fmt.Errorf("naming-glue-gate: DeepSeek response had no choices: %s", string(body))
	}

	var report Report
	if err := json.Unmarshal([]byte(parsed.Choices[0].Message.Content), &report); err != nil {
		return Report{}, fmt.Errorf("naming-glue-gate: model content was not the expected {\"findings\":[...]} JSON: %w (content: %s)", err, parsed.Choices[0].Message.Content)
	}
	for i := range report.Findings {
		report.Findings[i].Kind = "directory"
	}
	return report, nil
}
