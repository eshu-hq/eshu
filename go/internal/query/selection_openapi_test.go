// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	"github.com/eshu-hq/eshu/go/internal/scope/selection"
	"github.com/eshu-hq/eshu/go/internal/status"
)

func repositoryFreshnessResponseProperties(t *testing.T) map[string]any {
	t.Helper()
	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	route := testutil.MustMapField(t, testutil.MustMapField(t, spec, "paths"), "/api/v0/repositories/{repo_id}/freshness")
	ok := testutil.MustMapField(t, testutil.MustMapField(t, testutil.MustMapField(t, route, "get"), "responses"), "200")
	body := testutil.MustMapField(t, testutil.MustMapField(t, ok, "content"), "application/json")
	return testutil.MustMapField(t, testutil.MustMapField(t, body, "schema"), "properties")
}

// stringEnum returns the sorted string members of schema's enum and whether
// the enum also lists null.
func stringEnum(t *testing.T, schema map[string]any) ([]string, bool) {
	t.Helper()
	raw, ok := schema["enum"].([]any)
	if !ok {
		t.Fatalf("schema %#v has no enum", schema)
	}
	values := make([]string, 0, len(raw))
	hasNull := false
	for _, value := range raw {
		if value == nil {
			hasNull = true
			continue
		}
		values = append(values, value.(string))
	}
	slices.Sort(values)
	return values, hasNull
}

// TestOpenAPIRepositoryFreshnessVerdictEnumMatchesStatus keeps the documented
// verdict enum in lockstep with the verdicts status can compute.
func TestOpenAPIRepositoryFreshnessVerdictEnumMatchesStatus(t *testing.T) {
	t.Parallel()

	properties := repositoryFreshnessResponseProperties(t)
	want := []string{
		string(status.RepositoryFreshnessCurrent),
		string(status.RepositoryFreshnessBuilding),
		string(status.RepositoryFreshnessBehind),
		string(status.RepositoryFreshnessUnobserved),
		string(status.RepositoryFreshnessNotSelected),
		string(status.RepositoryFreshnessUnknown),
	}
	slices.Sort(want)
	if got, _ := stringEnum(t, testutil.MustMapField(t, properties, "verdict")); !slices.Equal(got, want) {
		t.Fatalf("verdict enum = %v, want %v", got, want)
	}
}

// TestOpenAPIRepositoryFreshnessSelectionSchema pins the #7625 selection
// object: always present, the exact keys the handler renders, and enums that
// match the selection package.
func TestOpenAPIRepositoryFreshnessSelectionSchema(t *testing.T) {
	t.Parallel()

	sel := testutil.MustMapField(t, repositoryFreshnessResponseProperties(t), "selection")
	if _, nullable := sel["nullable"]; nullable {
		t.Fatalf("selection.nullable = %#v, want absent: the block is always rendered, unknown when no row is live", sel["nullable"])
	}
	properties := testutil.MustMapField(t, sel, "properties")
	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	if want := []string{"evaluated_at", "last_listed_at", "live_selector_count", "reason", "state", "state_since"}; !slices.Equal(keys, want) {
		t.Fatalf("selection properties = %v, want %v", keys, want)
	}

	wantStates := []string{
		string(selection.AggregateExcludedStillIngested), string(selection.AggregateNotSelected),
		string(selection.AggregatePendingConfirmation), string(selection.AggregateSelected), string(selection.AggregateUnknown),
	}
	if got, _ := stringEnum(t, testutil.MustMapField(t, properties, "state")); !slices.Equal(got, wantStates) {
		t.Fatalf("selection.state enum = %v, want %v", got, wantStates)
	}
	if count := testutil.MustMapField(t, properties, "live_selector_count"); count["type"] != "integer" || count["nullable"] != nil {
		t.Fatalf("selection.live_selector_count = %#v, want a non-nullable integer", count)
	}
	reason := testutil.MustMapField(t, properties, "reason")
	wantReasons := []string{string(selection.StateArchivedExcluded), string(selection.StateNotListed), string(selection.StateRuleExcluded)}
	got, hasNull := stringEnum(t, reason)
	if !slices.Equal(got, wantReasons) {
		t.Fatalf("selection.reason enum = %v, want %v", got, wantReasons)
	}
	// OpenAPI 3.0.3: nullable does not override enum, so null must be listed.
	if reason["nullable"] != true || !hasNull {
		t.Fatalf("selection.reason = %#v, want nullable with null in its enum", reason)
	}
	for _, key := range []string{"last_listed_at", "state_since", "evaluated_at"} {
		if field := testutil.MustMapField(t, properties, key); field["nullable"] != true || field["format"] != "date-time" {
			t.Fatalf("selection.%s = %#v, want a nullable date-time", key, field)
		}
	}
}
