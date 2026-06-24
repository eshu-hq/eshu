// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package jira

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestLiveJiraWorkItemEvidence(t *testing.T) {
	if liveJiraEnvFirst("ESHU_JIRA_LIVE") != "1" {
		t.Skip("set ESHU_JIRA_LIVE=1 to run the live Jira work-item smoke")
	}

	target, secrets := liveJiraTarget(t)
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "collector-jira-live",
		Targets:             []TargetConfig{target},
		Now:                 time.Now,
	})
	if err != nil {
		liveJiraAssertNoSecrets(t, "NewClaimedSource error", err.Error(), secrets)
		t.Fatalf("NewClaimedSource() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	collected, ok, err := source.NextClaimed(ctx, workflow.WorkItem{
		WorkItemID:          "jira-live-work-item",
		CollectorKind:       scope.CollectorJira,
		CollectorInstanceID: "collector-jira-live",
		ScopeID:             target.ScopeID,
		GenerationID:        "jira:live",
		SourceRunID:         "jira:live",
		CurrentFencingToken: 1,
	})
	if err != nil {
		liveJiraAssertNoSecrets(t, "NextClaimed error", err.Error(), secrets)
		t.Fatalf("NextClaimed() error = %v", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}

	gotKinds := map[string]int{}
	for envelope := range collected.Facts {
		if !strings.HasPrefix(envelope.FactKind, "work_item.") {
			t.Fatalf("FactKind = %q, want work_item.*", envelope.FactKind)
		}
		gotKinds[envelope.FactKind]++
		liveJiraAssertEnvelopeSanitized(t, envelope, secrets)
	}
	if len(gotKinds) == 0 {
		t.Fatal("live Jira smoke emitted no work-item facts")
	}
}

func TestLiveJiraContainsSecretScansEnvelopeComposites(t *testing.T) {
	t.Parallel()

	secrets := []string{"secret-token"}
	envelope := facts.Envelope{
		FactID:        "safe",
		StableFactKey: "safe",
		Payload: map[string]any{
			"nested_strings": []string{"safe", "secret-token"},
			"nested_map": map[string]string{
				"authorization": "Bearer secret-token",
			},
			"stringer": liveJiraSecretStringer{value: "secret-token"},
		},
	}

	if !liveJiraContainsSecret(envelope, secrets) {
		t.Fatal("liveJiraContainsSecret() = false, want true for envelope composites")
	}
	if liveJiraContainsSecret(facts.Envelope{FactID: "safe"}, secrets) {
		t.Fatal("liveJiraContainsSecret() = true, want false for safe envelope")
	}
}

type liveJiraSecretStringer struct {
	value string
}

func (s liveJiraSecretStringer) String() string {
	return s.value
}

func liveJiraTarget(t *testing.T) (TargetConfig, []string) {
	t.Helper()

	baseURL := liveJiraRequiredEnv(t, "ESHU_JIRA_BASE_URL", "JIRA_BASE_URL")
	email := liveJiraRequiredEnv(t, "ESHU_JIRA_EMAIL", "JIRA_EMAIL")
	token := liveJiraRequiredEnv(t, "ESHU_JIRA_API_TOKEN", "JIRA_API_TOKEN")
	siteID := liveJiraEnvFirst("ESHU_JIRA_SITE_ID")
	if siteID == "" {
		siteID = liveJiraHost(t, baseURL)
	}
	scopeID := liveJiraEnvFirst("ESHU_JIRA_SCOPE_ID")
	if scopeID == "" {
		scopeID = "jira:site:" + siteID
	}
	lookback := liveJiraDurationEnv(t, "ESHU_JIRA_UPDATED_LOOKBACK", 168*time.Hour)
	return TargetConfig{
		Provider:        ProviderJiraCloud,
		ScopeID:         scopeID,
		SiteID:          siteID,
		BaseURL:         strings.TrimRight(baseURL, "/"),
		Email:           email,
		Token:           token,
		JQL:             liveJiraEnvFirst("ESHU_JIRA_JQL"),
		IssueLimit:      liveJiraIntEnv(t, "ESHU_JIRA_ISSUE_LIMIT", 1),
		UpdatedLookback: lookback,
		ChangelogLimit:  liveJiraIntEnv(t, "ESHU_JIRA_CHANGELOG_LIMIT", 10),
		RemoteLinkLimit: liveJiraIntEnv(t, "ESHU_JIRA_REMOTE_LINK_LIMIT", 10),
		MetadataLimit:   liveJiraIntEnv(t, "ESHU_JIRA_METADATA_LIMIT", 25),
	}, liveJiraSecrets(email, token)
}

func liveJiraHost(t *testing.T, rawURL string) string {
	t.Helper()

	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		t.Skip("set ESHU_JIRA_SITE_ID when ESHU_JIRA_BASE_URL is not an absolute URL")
	}
	return parsed.Host
}

func liveJiraRequiredEnv(t *testing.T, keys ...string) string {
	t.Helper()

	if value := liveJiraEnvFirst(keys...); value != "" {
		return value
	}
	t.Skipf("set one of %s to run the live Jira work-item smoke", strings.Join(keys, ", "))
	return ""
}

func liveJiraEnvFirst(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func liveJiraDurationEnv(t *testing.T, key string, fallback time.Duration) time.Duration {
	t.Helper()

	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		t.Fatalf("%s = %q, want Go duration: %v", key, raw, err)
	}
	return value
}

func liveJiraIntEnv(t *testing.T, key string, fallback int) int {
	t.Helper()

	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		t.Fatalf("%s = %q, want integer: %v", key, raw, err)
	}
	return value
}

func liveJiraSecrets(values ...string) []string {
	secrets := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if len(value) >= 3 {
			secrets = append(secrets, value)
		}
	}
	return secrets
}

func liveJiraAssertEnvelopeSanitized(t *testing.T, envelope facts.Envelope, secrets []string) {
	t.Helper()

	if strings.Contains(envelope.SourceRef.SourceURI, "?") ||
		strings.Contains(envelope.SourceRef.SourceURI, "#") {
		t.Fatalf("SourceRef.SourceURI = %q, want no query or fragment", envelope.SourceRef.SourceURI)
	}
	liveJiraAssertNoSecrets(t, envelope.FactKind+" envelope", envelope, secrets)
}

func liveJiraAssertNoSecrets(t *testing.T, label string, value any, secrets []string) {
	t.Helper()

	if liveJiraContainsSecret(value, secrets) {
		t.Fatalf("%s leaked live credential material", label)
	}
}

func liveJiraContainsSecret(value any, secrets []string) bool {
	if len(secrets) == 0 || value == nil {
		return false
	}
	return liveJiraValueContainsSecret(reflect.ValueOf(value), secrets, map[liveJiraVisit]bool{})
}

type liveJiraVisit struct {
	typ reflect.Type
	ptr uintptr
}

func liveJiraValueContainsSecret(value reflect.Value, secrets []string, seen map[liveJiraVisit]bool) bool {
	if !value.IsValid() {
		return false
	}
	for value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return false
		}
		if value.Kind() == reflect.Pointer {
			key := liveJiraVisit{typ: value.Type(), ptr: value.Pointer()}
			if seen[key] {
				return false
			}
			seen[key] = true
		}
		value = value.Elem()
	}
	if value.CanInterface() {
		if stringer, ok := value.Interface().(fmt.Stringer); ok &&
			liveJiraStringContainsSecret(stringer.String(), secrets) {
			return true
		}
		if err, ok := value.Interface().(error); ok &&
			liveJiraStringContainsSecret(err.Error(), secrets) {
			return true
		}
	}

	switch value.Kind() {
	case reflect.String:
		return liveJiraStringContainsSecret(value.String(), secrets)
	case reflect.Map:
		for _, key := range value.MapKeys() {
			if liveJiraValueContainsSecret(key, secrets, seen) ||
				liveJiraValueContainsSecret(value.MapIndex(key), secrets, seen) {
				return true
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < value.Len(); i++ {
			if liveJiraValueContainsSecret(value.Index(i), secrets, seen) {
				return true
			}
		}
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			if liveJiraValueContainsSecret(value.Field(i), secrets, seen) {
				return true
			}
		}
	default:
		if value.CanInterface() && liveJiraStringContainsSecret(fmt.Sprint(value.Interface()), secrets) {
			return true
		}
	}
	return false
}

func liveJiraStringContainsSecret(value string, secrets []string) bool {
	for _, secret := range secrets {
		if strings.Contains(value, secret) {
			return true
		}
	}
	return false
}
