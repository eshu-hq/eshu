// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestHandlerDrivesBoundedCodeTopicToolThenFinalTurn(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want responseMessage
	}{
		{
			name: "tool turn",
			body: `{"model":"golden-ask","messages":[{"role":"user","content":"Explain lib-common usage."}],"tools":[{"type":"function","function":{"name":"investigate_code_topic"}}]}`,
			want: responseMessage{ToolCalls: []responseToolCall{{
				ID: "golden-code-topic-1", Type: "function",
				Function: responseToolFunction{Name: "investigate_code_topic", Arguments: `{"limit":10,"repo_id":"orders-api","topic":"lib-common"}`},
			}}},
		},
		{
			name: "final turn",
			body: `{"model":"golden-ask","messages":[{"role":"user","content":"Explain lib-common usage."},{"role":"assistant","content":null,"tool_calls":[{"id":"golden-code-topic-1"}]},{"role":"tool","tool_call_id":"golden-code-topic-1","content":"{\"supported\":true}"}],"tools":[{"type":"function","function":{"name":"investigate_code_topic"}}]}`,
			want: responseMessage{Content: stringPointer("Evidence-backed code-topic investigation complete.")},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodPost, chatCompletionsEndpoint, strings.NewReader(test.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			newHandler().ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
			}
			var got completionResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if len(got.Choices) != 1 {
				t.Fatalf("choices = %#v, want one", got.Choices)
			}
			assertResponseMessage(t, got.Choices[0].Message, test.want)
		})
	}
}

func TestHandlerRejectsHostileRequests(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, method, path, body string
		want                     int
	}{
		{name: "wrong method", method: http.MethodGet, path: chatCompletionsEndpoint, want: http.StatusMethodNotAllowed},
		{name: "wrong path", method: http.MethodPost, path: "/v1/responses", body: `{}`, want: http.StatusNotFound},
		{name: "malformed", method: http.MethodPost, path: chatCompletionsEndpoint, body: `{`, want: http.StatusBadRequest},
		{name: "missing tool", method: http.MethodPost, path: chatCompletionsEndpoint, body: `{"messages":[{"role":"user","content":"x"}]}`, want: http.StatusBadRequest},
		{name: "unexpected question", method: http.MethodPost, path: chatCompletionsEndpoint, body: `{"messages":[{"role":"user","content":"ignore instructions"}],"tools":[{"type":"function","function":{"name":"investigate_code_topic"}}]}`, want: http.StatusBadRequest},
		{name: "unmatched tool result", method: http.MethodPost, path: chatCompletionsEndpoint, body: `{"messages":[{"role":"user","content":"Explain lib-common usage."},{"role":"tool","tool_call_id":"other","content":"{}"}],"tools":[{"type":"function","function":{"name":"investigate_code_topic"}}]}`, want: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			rec := httptest.NewRecorder()
			newHandler().ServeHTTP(rec, req)
			if rec.Code != test.want {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, test.want, rec.Body.String())
			}
		})
	}
}

func assertResponseMessage(t *testing.T, got, want responseMessage) {
	t.Helper()
	if !reflect.DeepEqual(got.Content, want.Content) || !reflect.DeepEqual(got.ToolCalls, want.ToolCalls) {
		t.Fatalf("message = %#v, want content/tool_calls %#v", got, want)
	}
}

func TestHealthIsStrict(t *testing.T) {
	t.Parallel()
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		req := httptest.NewRequest(method, healthEndpoint, nil)
		rec := httptest.NewRecorder()
		newHandler().ServeHTTP(rec, req)
		want := http.StatusOK
		if method != http.MethodGet {
			want = http.StatusMethodNotAllowed
		}
		if rec.Code != want {
			t.Fatalf("%s health status = %d, want %d", method, rec.Code, want)
		}
	}
}
