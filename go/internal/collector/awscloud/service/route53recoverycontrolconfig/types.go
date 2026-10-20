// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package route53recoverycontrolconfig

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client snapshots metadata-only Amazon Route 53 Application Recovery Controller
// recovery-control configuration observations for one AWS claim. Implementations
// read control-plane List/Describe APIs only and never read or set routing
// control state through the route53recoverycluster data plane.
type Client interface {
	// Snapshot returns every recovery-control cluster visible to the configured
	// AWS credentials, each carrying its control panels, and each control panel
	// carrying its routing controls and safety rules.
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures recovery-control configuration metadata plus non-fatal scan
// warnings.
type Snapshot struct {
	// Clusters is the metadata-only set of recovery-control clusters, each
	// carrying its control panels.
	Clusters []Cluster
	// Warnings carries non-fatal partial-scan observations such as sustained
	// throttling that omitted a metadata component.
	Warnings []awscloud.WarningObservation
}

// Cluster is the scanner-owned recovery-control cluster model. It carries
// control-plane metadata only; the cluster's regional endpoints are recorded as
// Region names for context, never used to read or set routing control state.
type Cluster struct {
	// ARN is the Amazon Resource Name that uniquely identifies the cluster. It is
	// reported by AWS already partition-correct.
	ARN string
	// Name is the cluster name.
	Name string
	// Status is the cluster deployment status (PENDING, DEPLOYED,
	// PENDING_DELETION).
	Status string
	// NetworkType is the cluster network type (for example IPV4 or DUALSTACK).
	NetworkType string
	// Owner is the AWS account id that owns the cluster.
	Owner string
	// EndpointRegions are the AWS Regions of the cluster's regional endpoints.
	// Endpoint URLs are intentionally excluded so the scanner cannot be used to
	// reach the routing control state data plane.
	EndpointRegions []string
	// Tags carries the cluster resource tags.
	Tags map[string]string
	// ControlPanels are the metadata-only control panels that live under this
	// cluster.
	ControlPanels []ControlPanel
}

// ControlPanel is the scanner-owned recovery-control control panel model.
type ControlPanel struct {
	// ARN is the Amazon Resource Name that uniquely identifies the control panel.
	ARN string
	// ClusterARN is the ARN of the owning cluster.
	ClusterARN string
	// Name is the control panel name.
	Name string
	// Status is the control panel deployment status.
	Status string
	// DefaultControlPanel reports whether this is the cluster's default control
	// panel.
	DefaultControlPanel bool
	// RoutingControlCount is the number of routing controls AWS reports in the
	// control panel.
	RoutingControlCount int32
	// Owner is the AWS account id that owns the control panel.
	Owner string
	// Tags carries the control panel resource tags.
	Tags map[string]string
	// RoutingControls are the metadata-only routing controls under this panel.
	RoutingControls []RoutingControl
	// SafetyRules are the metadata-only safety rules that guard this panel.
	SafetyRules []SafetyRule
}

// RoutingControl is the scanner-owned recovery-control routing control model. It
// records identity and lifecycle only; the live On/Off state is never read.
type RoutingControl struct {
	// ARN is the Amazon Resource Name that uniquely identifies the routing
	// control.
	ARN string
	// ControlPanelARN is the ARN of the owning control panel.
	ControlPanelARN string
	// Name is the routing control name.
	Name string
	// Status is the routing control deployment status.
	Status string
	// Owner is the AWS account id that owns the routing control.
	Owner string
	// Tags carries the routing control resource tags.
	Tags map[string]string
}

// SafetyRule is the scanner-owned recovery-control safety rule model. A safety
// rule is either an assertion rule or a gating rule; the scanner records the
// rule logic and routing control counts only, never application traffic.
type SafetyRule struct {
	// ARN is the Amazon Resource Name that uniquely identifies the safety rule.
	ARN string
	// ControlPanelARN is the ARN of the control panel the rule guards.
	ControlPanelARN string
	// Name is the safety rule name.
	Name string
	// RuleKind distinguishes the rule shape: "ASSERTION" or "GATING".
	RuleKind string
	// Status is the safety rule deployment status.
	Status string
	// WaitPeriodMs is the evaluation wait period in milliseconds.
	WaitPeriodMs int32
	// RuleConfigType is the rule-config logic type (ATLEAST, AND, OR).
	RuleConfigType string
	// RuleConfigThreshold is the N value for an ATLEAST rule type.
	RuleConfigThreshold int32
	// RuleConfigInverted reports whether the rule's evaluation is logically
	// negated.
	RuleConfigInverted bool
	// AssertedControlCount is the number of asserted routing controls (assertion
	// rules only).
	AssertedControlCount int
	// GatingControlCount is the number of gating routing controls (gating rules
	// only).
	GatingControlCount int
	// TargetControlCount is the number of target routing controls (gating rules
	// only).
	TargetControlCount int
	// Tags carries the safety rule resource tags.
	Tags map[string]string
}
