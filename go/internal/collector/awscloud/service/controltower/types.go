// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package controltower

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client snapshots metadata-only AWS Control Tower landing-zone, enabled-control,
// and enabled-baseline observations for one AWS claim. Implementations read
// control-plane list/get APIs only and never read or persist the landing-zone
// manifest body, control or baseline parameter values, or any governance
// payload, and never enable, disable, reset, or otherwise mutate Control Tower
// state.
type Client interface {
	// Snapshot returns the Control Tower landing zone (if any), the enabled
	// baselines, and the enabled controls governing the organization visible to
	// the configured AWS credentials.
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures Control Tower control-plane metadata plus non-fatal scan
// warnings. A management account governs at most one landing zone, so LandingZone
// is a pointer that is nil when Control Tower is not set up in the boundary.
type Snapshot struct {
	// LandingZone is the metadata-only landing zone for the boundary, or nil when
	// no landing zone is deployed.
	LandingZone *LandingZone
	// EnabledControls is the metadata-only set of enabled controls and the
	// organizational-unit targets they govern.
	EnabledControls []EnabledControl
	// EnabledBaselines is the metadata-only set of enabled baselines and the
	// targets they govern.
	EnabledBaselines []EnabledBaseline
	// Warnings carries non-fatal partial-scan observations such as sustained
	// throttling that omitted a metadata component.
	Warnings []awscloud.WarningObservation
}

// LandingZone is the scanner-owned Control Tower landing-zone model. It carries
// control-plane metadata only and intentionally excludes the landing-zone
// manifest JSON body, which holds governance configuration.
type LandingZone struct {
	// ARN is the Amazon Resource Name that uniquely identifies the landing zone.
	ARN string
	// Version is the landing zone's currently deployed version.
	Version string
	// LatestAvailableVersion is the latest landing-zone version AWS reports as
	// available, when present.
	LatestAvailableVersion string
	// Status is the landing zone's deployment status (for example ACTIVE).
	Status string
	// DriftStatus is the landing zone's drift status (for example IN_SYNC), when
	// reported.
	DriftStatus string
	// Tags carries the landing-zone resource tags.
	Tags map[string]string
}

// EnabledControl is the scanner-owned Control Tower enabled-control model. It
// carries control-plane metadata only and intentionally excludes control
// parameter values.
type EnabledControl struct {
	// ARN is the Amazon Resource Name that uniquely identifies the enabled
	// control.
	ARN string
	// ControlIdentifier is the identifier (ARN) of the control definition that is
	// enabled.
	ControlIdentifier string
	// TargetIdentifier is the ARN of the Organizations target (organizational
	// unit) the control is enabled on.
	TargetIdentifier string
	// ParentIdentifier is the ARN of the parent enabled control this control
	// inherits configuration from, when applicable.
	ParentIdentifier string
	// Status is the enabled control's deployment status (for example SUCCEEDED),
	// when reported.
	Status string
	// DriftStatus is the enabled control's drift status (for example IN_SYNC),
	// when reported.
	DriftStatus string
}

// EnabledBaseline is the scanner-owned Control Tower enabled-baseline model. It
// carries control-plane metadata only and intentionally excludes baseline
// parameter values.
type EnabledBaseline struct {
	// ARN is the Amazon Resource Name that uniquely identifies the enabled
	// baseline.
	ARN string
	// BaselineIdentifier is the identifier (ARN) of the baseline definition that
	// is enabled.
	BaselineIdentifier string
	// BaselineVersion is the enabled version of the baseline, when reported.
	BaselineVersion string
	// TargetIdentifier is the ARN of the Organizations target (organizational
	// unit, account, or root) the baseline is enabled on.
	TargetIdentifier string
	// ParentIdentifier is the ARN of the parent enabled baseline, when applicable.
	ParentIdentifier string
	// Status is the enabled baseline's deployment status (for example SUCCEEDED),
	// when reported.
	Status string
	// DriftStatus is the enabled baseline's drift status, when reported.
	DriftStatus string
}
