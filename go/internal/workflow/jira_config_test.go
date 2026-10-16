// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateJiraCollectorConfigurationAcceptsJQLEnvReference(t *testing.T) {
	t.Parallel()

	raw := `{"targets":[{
		"provider":"jira_cloud",
		"scope_id":"jira:site:example",
		"site_id":"example.atlassian.net",
		"base_url":"https://example.atlassian.net",
		"token_env":"JIRA_API_TOKEN",
		"jql_env":"ESHU_JIRA_JQL",
		"issue_limit":25,
		"updated_lookback":"24h",
		"changelog_limit":25,
		"remote_link_limit":25
	}]}`

	if err := ValidateJiraCollectorConfiguration(raw); err != nil {
		t.Fatalf("ValidateJiraCollectorConfiguration() error = %v, want nil", err)
	}
}

func TestValidateJiraCollectorConfigurationRejectsTargetWithoutJQLSource(t *testing.T) {
	t.Parallel()

	raw := `{"targets":[{
		"provider":"jira_cloud",
		"scope_id":"jira:site:example",
		"site_id":"example.atlassian.net",
		"base_url":"https://example.atlassian.net",
		"token_env":"JIRA_API_TOKEN",
		"issue_limit":25,
		"updated_lookback":"24h",
		"changelog_limit":25,
		"remote_link_limit":25
	}]}`

	err := ValidateJiraCollectorConfiguration(raw)
	if err == nil {
		t.Fatal("ValidateJiraCollectorConfiguration() error = nil, want missing JQL source")
	}
	if !strings.Contains(err.Error(), "jql or jql_env is required") {
		t.Fatalf("ValidateJiraCollectorConfiguration() error = %v, want jql source message", err)
	}
}

func TestValidateJiraCollectorConfigurationRoundTripsAllowEmptySuccessor(t *testing.T) {
	t.Parallel()

	raw := `{"targets":[{
		"provider":"jira_cloud",
		"scope_id":"jira:site:example",
		"site_id":"example.atlassian.net",
		"base_url":"https://example.atlassian.net",
		"token_env":"JIRA_API_TOKEN",
		"jql":"project = OPS",
		"allow_empty_successor":true
	}]}`

	if err := ValidateJiraCollectorConfiguration(raw); err != nil {
		t.Fatalf("ValidateJiraCollectorConfiguration() error = %v, want nil", err)
	}
	var decoded jiraCollectorConfiguration
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if len(decoded.Targets) != 1 {
		t.Fatalf("len(Targets) = %d, want 1", len(decoded.Targets))
	}
	if !decoded.Targets[0].AllowEmptySuccessor {
		t.Fatal("AllowEmptySuccessor = false with allow_empty_successor:true, want true")
	}

	absent := `{"targets":[{
		"provider":"jira_cloud",
		"scope_id":"jira:site:example",
		"site_id":"example.atlassian.net",
		"base_url":"https://example.atlassian.net",
		"token_env":"JIRA_API_TOKEN",
		"jql":"project = OPS"
	}]}`
	var decodedAbsent jiraCollectorConfiguration
	if err := json.Unmarshal([]byte(absent), &decodedAbsent); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if decodedAbsent.Targets[0].AllowEmptySuccessor {
		t.Fatal("AllowEmptySuccessor = true without allow_empty_successor, want false default")
	}
}

func TestValidateJiraCollectorConfigurationRejectsAmbiguousJQLSource(t *testing.T) {
	t.Parallel()

	raw := `{"targets":[{
		"provider":"jira_cloud",
		"scope_id":"jira:site:example",
		"site_id":"example.atlassian.net",
		"base_url":"https://example.atlassian.net",
		"token_env":"JIRA_API_TOKEN",
		"jql":"project = OPS",
		"jql_env":"ESHU_JIRA_JQL",
		"issue_limit":25,
		"updated_lookback":"24h",
		"changelog_limit":25,
		"remote_link_limit":25
	}]}`

	err := ValidateJiraCollectorConfiguration(raw)
	if err == nil {
		t.Fatal("ValidateJiraCollectorConfiguration() error = nil, want ambiguous JQL source")
	}
	if !strings.Contains(err.Error(), "only one of jql or jql_env may be set") {
		t.Fatalf("ValidateJiraCollectorConfiguration() error = %v, want ambiguous JQL source message", err)
	}
}
