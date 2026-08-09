// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

const (
	healthEndpoint          = "/health"
	chatCompletionsEndpoint = "/v1/chat/completions"
	fixtureQuestion         = "Explain lib-common usage."
	fixtureTool             = "investigate_code_topic"
	fixtureToolCallID       = "golden-code-topic-1"
)

type completionRequest struct {
	Model    string           `json:"model"`
	Messages []requestMessage `json:"messages"`
	Tools    []requestTool    `json:"tools"`
	Stream   bool             `json:"stream"`
}

type requestMessage struct {
	Role       string `json:"role"`
	Content    any    `json:"content"`
	ToolCallID string `json:"tool_call_id"`
}

type requestTool struct {
	Type     string              `json:"type"`
	Function requestToolFunction `json:"function"`
}

type requestToolFunction struct {
	Name string `json:"name"`
}

type completionResponse struct {
	Choices []responseChoice `json:"choices"`
	Usage   responseUsage    `json:"usage"`
}

type responseChoice struct {
	Message      responseMessage `json:"message"`
	FinishReason string          `json:"finish_reason"`
}

type responseMessage struct {
	Role      string             `json:"role"`
	Content   *string            `json:"content"`
	ToolCalls []responseToolCall `json:"tool_calls,omitempty"`
}

type responseToolCall struct {
	ID       string               `json:"id"`
	Type     string               `json:"type"`
	Function responseToolFunction `json:"function"`
}

type responseToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type responseUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func newHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+healthEndpoint, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("POST "+chatCompletionsEndpoint, handleChatCompletion)
	return mux
}

func handleChatCompletion(w http.ResponseWriter, r *http.Request) {
	var request completionRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := decoder.Decode(&request); err != nil {
		http.Error(w, "invalid OpenAI-compatible request", http.StatusBadRequest)
		return
	}
	if request.Stream || !hasFixtureQuestion(request.Messages) || !hasFixtureTool(request.Tools) {
		http.Error(w, "request is outside the closed golden Ask proof", http.StatusBadRequest)
		return
	}

	message := responseMessage{Role: "assistant"}
	finishReason := "stop"
	if hasFixtureToolResult(request.Messages) {
		message.Content = stringPointer("Evidence-backed code-topic investigation complete.")
	} else if hasAnyToolResult(request.Messages) {
		http.Error(w, "tool result does not match the golden tool call", http.StatusBadRequest)
		return
	} else {
		finishReason = "tool_calls"
		message.ToolCalls = []responseToolCall{{
			ID:   fixtureToolCallID,
			Type: "function",
			Function: responseToolFunction{
				Name:      fixtureTool,
				Arguments: `{"limit":10,"repo_id":"orders-api","topic":"lib-common"}`,
			},
		}}
	}
	writeJSON(w, completionResponse{
		Choices: []responseChoice{{Message: message, FinishReason: finishReason}},
		Usage:   responseUsage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2},
	})
}

func hasFixtureQuestion(messages []requestMessage) bool {
	for _, message := range messages {
		if message.Role == "user" {
			content, ok := message.Content.(string)
			return ok && strings.TrimSpace(content) == fixtureQuestion
		}
	}
	return false
}

func hasFixtureTool(tools []requestTool) bool {
	for _, tool := range tools {
		if tool.Type == "function" && tool.Function.Name == fixtureTool {
			return true
		}
	}
	return false
}

func hasFixtureToolResult(messages []requestMessage) bool {
	for _, message := range messages {
		if message.Role == "tool" && message.ToolCallID == fixtureToolCallID {
			return true
		}
	}
	return false
}

func hasAnyToolResult(messages []requestMessage) bool {
	for _, message := range messages {
		if message.Role == "tool" {
			return true
		}
	}
	return false
}

func stringPointer(value string) *string {
	return &value
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(value)
}
