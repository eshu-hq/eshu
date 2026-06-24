// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package jira

import (
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestWorkItemProjectAndFieldMetadataEnvelopesRedactPrivateValues(t *testing.T) {
	t.Parallel()

	ctx := testEnvelopeContext()
	project, err := NewWorkItemProjectMetadataEnvelope(ctx, ProjectMetadata{
		ID:              "10000",
		Key:             "OPS",
		Name:            "Private Operations",
		TypeKey:         "software",
		CategoryID:      "20000",
		CategoryName:    "Regulated Customers",
		Style:           "classic",
		Archived:        true,
		Self:            "https://example.atlassian.net/rest/api/3/project/OPS?accountId=acct-private&token=secret",
		LastIssueUpdate: ctx.ObservedAt,
		IssueCount:      42,
	})
	if err != nil {
		t.Fatalf("NewWorkItemProjectMetadataEnvelope() error = %v, want nil", err)
	}
	if project.FactKind != facts.WorkItemProjectMetadataFactKind {
		t.Fatalf("FactKind = %q, want %q", project.FactKind, facts.WorkItemProjectMetadataFactKind)
	}
	if got := project.Payload["project_name"]; got != "" {
		t.Fatalf("project_name = %q, want redacted", got)
	}
	if got := project.Payload["project_name_fingerprint"]; got == "" {
		t.Fatal("project_name_fingerprint is blank")
	}
	if got := project.Payload["visibility_state"]; got != "archived" {
		t.Fatalf("visibility_state = %q, want archived", got)
	}
	assertMetadataPayloadOmits(t, project.Payload, "Private Operations", "acct-private", "token=secret")

	field, err := NewWorkItemFieldMetadataEnvelope(ctx, FieldMetadata{
		ID:          "customfield_10042",
		Name:        "Customer account owner",
		Description: "owner@example.com should not be emitted",
		Schema: FieldSchema{
			Type:     "array",
			Items:    "user",
			Custom:   "com.atlassian.jira.plugin.system.customfieldtypes:multiuserpicker",
			CustomID: "10042",
		},
		Self: "https://example.atlassian.net/rest/api/3/field/customfield_10042?accountId=acct-private",
	})
	if err != nil {
		t.Fatalf("NewWorkItemFieldMetadataEnvelope() error = %v, want nil", err)
	}
	if field.FactKind != facts.WorkItemFieldMetadataFactKind {
		t.Fatalf("FactKind = %q, want %q", field.FactKind, facts.WorkItemFieldMetadataFactKind)
	}
	if got := field.Payload["field_name"]; got != "" {
		t.Fatalf("field_name = %q, want redacted", got)
	}
	if got := field.Payload["field_name_fingerprint"]; got == "" {
		t.Fatal("field_name_fingerprint is blank")
	}
	if got := field.Payload["custom_id_present"]; got != true {
		t.Fatalf("custom_id_present = %v, want true", got)
	}
	assertMetadataPayloadOmits(t, field.Payload, "Customer account owner", "owner@example.com", "customfield_10042", "10042", "acct-private")
}

func TestWorkItemWorkflowMetadataEnvelopeCarriesTransitionShapeWithoutRawNames(t *testing.T) {
	t.Parallel()

	env, err := NewWorkItemWorkflowMetadataEnvelope(testEnvelopeContext(), WorkflowMetadata{
		ID:      "workflow-1",
		Name:    "Customer Incident Workflow",
		Version: WorkflowVersion{ID: "version-1", Number: 3},
		Scope:   MetadataScope{Type: "PROJECT", ProjectID: "10000"},
		Statuses: []WorkflowStatusMetadata{
			{StatusReference: "todo-ref", StatusID: "10001", Deprecated: false},
			{StatusReference: "done-ref", StatusID: "10002", Deprecated: false},
		},
		Transitions: []WorkflowTransitionMetadata{
			{
				ID:                   "41",
				Name:                 "Resolve customer issue",
				Type:                 "DIRECTED",
				FromStatusReferences: []string{"todo-ref"},
				ToStatusReference:    "done-ref",
				HasValidators:        true,
			},
		},
	})
	if err != nil {
		t.Fatalf("NewWorkItemWorkflowMetadataEnvelope() error = %v, want nil", err)
	}
	if env.FactKind != facts.WorkItemWorkflowMetadataFactKind {
		t.Fatalf("FactKind = %q, want %q", env.FactKind, facts.WorkItemWorkflowMetadataFactKind)
	}
	transitions, ok := env.Payload["transitions"].([]map[string]any)
	if !ok || len(transitions) != 1 {
		t.Fatalf("transitions = %#v, want one sanitized transition", env.Payload["transitions"])
	}
	if got := transitions[0]["transition_name"]; got != "" {
		t.Fatalf("transition_name = %q, want redacted", got)
	}
	if got := transitions[0]["transition_name_fingerprint"]; got == "" {
		t.Fatal("transition_name_fingerprint is blank")
	}
	if got := transitions[0]["has_validators"]; got != true {
		t.Fatalf("has_validators = %v, want true", got)
	}
	assertMetadataPayloadOmits(t, env.Payload, "Customer Incident Workflow", "Resolve customer issue")
}

func TestWorkItemMetadataWarningEnvelopeDistinguishesPermissionHidden(t *testing.T) {
	t.Parallel()

	env, err := NewWorkItemMetadataWarningEnvelope(testEnvelopeContext(), MetadataWarning{
		MetadataType: "workflow",
		Reason:       "permission_hidden",
		FailureClass: string(FailurePermissionHidden),
		ProviderID:   "workflow-1",
	})
	if err != nil {
		t.Fatalf("NewWorkItemMetadataWarningEnvelope() error = %v, want nil", err)
	}
	if env.FactKind != facts.WorkItemMetadataWarningFactKind {
		t.Fatalf("FactKind = %q, want %q", env.FactKind, facts.WorkItemMetadataWarningFactKind)
	}
	if got := env.Payload["reason"]; got != "permission_hidden" {
		t.Fatalf("reason = %q, want permission_hidden", got)
	}
}

func assertMetadataPayloadOmits(t *testing.T, payload map[string]any, forbidden ...string) {
	t.Helper()
	rendered := fmt.Sprint(payload)
	for _, value := range forbidden {
		if strings.Contains(rendered, value) {
			t.Fatalf("payload contains forbidden value %q: %#v", value, payload)
		}
	}
}
