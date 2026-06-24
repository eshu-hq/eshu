// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudformation

import (
	"context"
	"time"
)

// Client is the minimal CloudFormation read surface consumed by Scanner.
//
// The interface is the scanner-level security boundary. It deliberately
// excludes every mutation API (Create/Update/Delete Stack, Create/Update/Delete
// StackSet, Create/Execute/Delete ChangeSet, RollbackStack,
// ContinueUpdateRollback, CancelUpdateStack, Create/Delete StackInstances) and
// the template-body extraction APIs (GetTemplate, GetTemplateSummary). The
// scanner never reads a template body, parameter values, change-set bodies, or
// drift property documents. TestClientInterfaceExcludesMutationAndTemplateAPIs
// fails the build if any forbidden method is added here.
type Client interface {
	// ListStacks returns active and recently deleted stack metadata.
	ListStacks(context.Context) ([]Stack, error)
	// ListStackResources returns the per-stack resource type summary used for
	// stack-to-resource-type relationships. It returns ARN and type only.
	ListStackResources(ctx context.Context, stackID string) ([]StackResource, error)
	// ListStackSets returns stack-set metadata. Implementations must never carry
	// the stack-set TemplateBody into the returned records.
	ListStackSets(context.Context) ([]StackSet, error)
	// ListChangeSets returns change-set metadata for one stack. The per-resource
	// change body is never read.
	ListChangeSets(ctx context.Context, stackID string) ([]ChangeSet, error)
	// ListStackResourceDrifts returns drift detection result metadata for one
	// stack as per-status counts. Actual and expected property documents are
	// never carried into the returned record.
	ListStackResourceDrifts(ctx context.Context, stackID string) (StackDriftResult, error)
	// ListStackInstances returns stack-instance metadata for one stack set.
	ListStackInstances(ctx context.Context, stackSetName string) ([]StackInstance, error)
	// ListTypes returns registered extension (type) metadata.
	ListTypes(context.Context) ([]RegisteredType, error)
}

// Stack is the scanner-owned representation of one CloudFormation stack. It
// carries identity, status, capabilities, role ARN, and template URL reference
// only. The template body, parameter values, and secret-like output values are
// intentionally outside this contract.
type Stack struct {
	ID                          string
	Name                        string
	Status                      string
	StatusReason                string
	Description                 string
	RoleARN                     string
	TemplateURL                 string
	Capabilities                []string
	NotificationARNs            []string
	ParentID                    string
	RootID                      string
	ChangeSetID                 string
	DriftStatus                 string
	EnableTerminationProtection bool
	DisableRollback             bool
	Deleted                     bool
	ParameterKeys               []string
	Outputs                     []StackOutput
	Tags                        map[string]string
	CreationTime                time.Time
	LastUpdatedTime             time.Time
	DeletionTime                time.Time
}

// StackOutput is one CloudFormation stack output reference reported by the SDK
// adapter. It carries the output key, export name, description, and raw value.
// The scanner is responsible for redacting secret-like values before emission;
// the SDK adapter performs no classification of its own.
type StackOutput struct {
	Key         string
	ExportName  string
	Description string
	// Value is the raw output value as reported by CloudFormation. The scanner
	// replaces it with a redaction marker when the output key matches the
	// shared AWS sensitive-key policy, so a secret-shaped value never reaches a
	// durable fact.
	Value string
}

// StackResource is the scanner-owned summary of one resource managed by a
// stack. It carries the resource type and physical/logical identity only; no
// resource property body is read.
type StackResource struct {
	LogicalID    string
	PhysicalID   string
	ResourceType string
	Status       string
	DriftStatus  string
}

// StackSet is the scanner-owned representation of one CloudFormation stack set.
// It carries identity, status, capabilities, permission model, and role
// references only. The stack-set template body and parameter values are
// intentionally outside this contract.
type StackSet struct {
	ID                    string
	Name                  string
	ARN                   string
	Status                string
	Description           string
	PermissionModel       string
	AdministrationRoleARN string
	ExecutionRoleName     string
	DriftStatus           string
	Capabilities          []string
	OrganizationalUnitIDs []string
	Regions               []string
	ParameterKeys         []string
	Tags                  map[string]string
}

// ChangeSet is the scanner-owned representation of one CloudFormation change
// set. It carries change-set identity and status only. The per-resource change
// body (action, replacement detail) is never read or persisted.
type ChangeSet struct {
	ID              string
	Name            string
	StackID         string
	StackName       string
	Status          string
	StatusReason    string
	ExecutionStatus string
	Description     string
	CreationTime    time.Time
}

// StackDriftResult is the scanner-owned drift detection summary for one stack.
// It carries per-status resource counts only; actual and expected property
// documents and per-property differences are never persisted.
type StackDriftResult struct {
	StackID         string
	TotalChecked    int
	DriftedCount    int
	InSyncCount     int
	NotCheckedCount int
	DeletedCount    int
	ModifiedCount   int
}

// StackInstance is the scanner-owned representation of one stack-set instance.
// It carries account, region, and status only.
type StackInstance struct {
	StackSetID   string
	StackSetName string
	StackID      string
	Account      string
	Region       string
	Status       string
	StatusReason string
	DriftStatus  string
}

// RegisteredType is the scanner-owned representation of one CloudFormation
// registered extension (type). It carries type identity, kind, default version,
// publisher, and activation state only. Type schema and configuration bodies
// are never persisted.
type RegisteredType struct {
	ARN              string
	TypeName         string
	Kind             string
	DefaultVersionID string
	PublisherID      string
	PublisherName    string
	IsActivated      bool
	LastUpdated      time.Time
}
