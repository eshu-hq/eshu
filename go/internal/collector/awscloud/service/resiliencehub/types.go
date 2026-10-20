// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package resiliencehub

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client snapshots metadata-only AWS Resilience Hub observations for one AWS
// claim. Implementations read control-plane describe/list APIs only and never
// read assessment result bodies, drift detail, recommendation contents, or any
// data-plane payload.
type Client interface {
	// Snapshot returns every Resilience Hub application visible to the
	// configured AWS credentials (each carrying its policy reference, input
	// sources, components, protected physical resources, and assessment
	// summaries) plus the account's resiliency policies.
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures Resilience Hub application and policy metadata plus
// non-fatal scan warnings.
type Snapshot struct {
	// Apps is the metadata-only set of Resilience Hub applications.
	Apps []App
	// Policies is the metadata-only set of account-level resiliency policies.
	Policies []ResiliencyPolicy
	// Warnings carries non-fatal partial-scan observations such as sustained
	// throttling or a missing published application version that omitted a
	// metadata component.
	Warnings []awscloud.WarningObservation
}

// App is the scanner-owned Resilience Hub application model. It carries
// control-plane metadata only.
type App struct {
	// ARN is the Amazon Resource Name that uniquely identifies the application.
	ARN string
	// Name is the application name.
	Name string
	// Description is the optional application description.
	Description string
	// Status is the application lifecycle status (for example Active).
	Status string
	// ComplianceStatus is the reported compliance status label.
	ComplianceStatus string
	// DriftStatus is the reported drift status label.
	DriftStatus string
	// AssessmentSchedule is the configured assessment schedule (Daily/Disabled).
	AssessmentSchedule string
	// PolicyARN is the ARN of the resiliency policy governing the application,
	// when one is attached.
	PolicyARN string
	// AWSApplicationARN is the ARN of an integrated AppRegistry application,
	// when one is attached.
	AWSApplicationARN string
	// ResiliencyScore is the current resiliency score for the application.
	ResiliencyScore float64
	// RPOInSecs is the configured Recovery Point Objective target in seconds.
	RPOInSecs *int32
	// RTOInSecs is the configured Recovery Time Objective target in seconds.
	RTOInSecs *int32
	// CreationTime is when the application was created.
	CreationTime time.Time
	// Tags carries the application resource tags.
	Tags map[string]string
	// InputSources are the metadata-only input sources the application draws
	// its resources from.
	InputSources []InputSource
	// Components are the metadata-only application components.
	Components []AppComponent
	// ProtectedResources are the published-version physical resources whose
	// identifier Resilience Hub reports as an ARN. Native (non-ARN) identifiers
	// are intentionally excluded from this slice so protected-resource edges
	// never dangle.
	ProtectedResources []ProtectedResource
	// Assessments are the metadata-only assessment summaries run for the
	// application.
	Assessments []Assessment
}

// ResiliencyPolicy is the scanner-owned Resilience Hub resiliency policy model.
type ResiliencyPolicy struct {
	// ARN is the Amazon Resource Name that uniquely identifies the policy.
	ARN string
	// Name is the policy name.
	Name string
	// Description is the optional policy description.
	Description string
	// Tier is the policy tier (for example MissionCritical or NonCritical).
	Tier string
	// EstimatedCostTier is the reported estimated cost tier.
	EstimatedCostTier string
	// DataLocationConstraint is the geographical data-location constraint.
	DataLocationConstraint string
	// FailureTargets holds the per-failure-type RPO/RTO targets keyed by the
	// failure policy name (AZ, Hardware, Software, Region). It carries only the
	// numeric objectives, never any customer data.
	FailureTargets map[string]FailureTarget
	// CreationTime is when the policy was created.
	CreationTime time.Time
	// Tags carries the policy resource tags.
	Tags map[string]string
}

// FailureTarget is the RPO/RTO objective pair Resilience Hub reports for one
// failure type within a resiliency policy.
type FailureTarget struct {
	// RPOInSecs is the Recovery Point Objective target in seconds.
	RPOInSecs int32
	// RTOInSecs is the Recovery Time Objective target in seconds.
	RTOInSecs int32
}

// InputSource is the scanner-owned Resilience Hub application input source
// model. It identifies where the application draws its resources from.
type InputSource struct {
	// ImportType is the input source resource-mapping type (for example
	// CfnStack, Resource, AppRegistryApp, Terraform, or EKS).
	ImportType string
	// SourceName is the input source name.
	SourceName string
	// SourceARN is the ARN of the input source, when one is reported.
	SourceARN string
	// ResourceCount is the number of resources the input source contributes.
	ResourceCount int32
}

// AppComponent is the scanner-owned Resilience Hub application component model.
type AppComponent struct {
	// Name is the application component name.
	Name string
	// Type is the application component type (for example AWS::ResilienceHub::
	// ComputeAppComponent).
	Type string
}

// ProtectedResource is one published-version physical resource Resilience Hub
// reports for an application by an ARN identifier. The scanner keeps it only
// when the identifier is ARN-shaped so the protected-resource edge joins the
// owning resource scanner's node.
type ProtectedResource struct {
	// ARN is the physical resource ARN Resilience Hub reports.
	ARN string
	// ResilienceHubType is the Resilience Hub-reported resource type string (for
	// example AWS::Lambda::Function). It selects the Eshu target resource type.
	ResilienceHubType string
	// LogicalResourceID is the logical identifier Resilience Hub assigns the
	// resource within the application, recorded as edge context.
	LogicalResourceID string
}

// Assessment is the scanner-owned Resilience Hub application assessment summary
// model. It carries only the assessment outcome labels, never the assessment
// result body or drift detail.
type Assessment struct {
	// ARN is the Amazon Resource Name that uniquely identifies the assessment.
	ARN string
	// AppARN is the ARN of the application the assessment was run for.
	AppARN string
	// Name is the assessment name.
	Name string
	// Status is the assessment lifecycle status (for example Success).
	Status string
	// ComplianceStatus is the reported compliance status label.
	ComplianceStatus string
	// DriftStatus is the reported drift status label.
	DriftStatus string
	// Invoker is who invoked the assessment (User or System).
	Invoker string
	// AppVersion is the application version the assessment evaluated.
	AppVersion string
	// ResiliencyScore is the resiliency score the assessment produced.
	ResiliencyScore float64
	// StartTime is when the assessment started.
	StartTime time.Time
	// EndTime is when the assessment ended.
	EndTime time.Time
}
