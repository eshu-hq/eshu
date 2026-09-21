// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package securityhub

import (
	"context"
	"time"
)

// Client returns one metadata-only Security Hub snapshot for a claimed account
// and region.
type Client interface {
	Snapshot(context.Context) (Snapshot, error)
}

// Snapshot is the scanner-owned Security Hub view used for fact emission.
type Snapshot struct {
	Hub           Hub
	Members       []Member
	Standards     []Standard
	ActionTargets []ActionTarget
	Insights      []Insight
	FindingCounts []FindingCount
}

// Hub is the metadata-only scanner view of a Security Hub hub.
type Hub struct {
	ARN                     string
	AutoEnableControls      bool
	ControlFindingGenerator string
	SubscribedAt            time.Time
	Tags                    map[string]string
	AdministratorAccountID  string
	AdministratorStatus     string
	MemberEnumerationStatus string
}

// Member is the metadata-only scanner view of a Security Hub member account.
type Member struct {
	AccountID       string
	AdministratorID string
	Status          string
	InvitedAt       time.Time
	UpdatedAt       time.Time
}

// Standard is the metadata-only scanner view of an enabled Security Hub
// standards subscription.
type Standard struct {
	ARN                     string
	SubscriptionARN         string
	Status                  string
	ControlsUpdatable       string
	StatusReasonCode        string
	StandardsInputKeys      []string
	Tags                    map[string]string
	ControlFindingGenerator string
	Controls                []Control
}

// Control is the metadata-only scanner view of one Security Hub standard
// control.
type Control struct {
	ARN              string
	ID               string
	Title            string
	ControlStatus    string
	SeverityRating   string
	Related          []string
	ComplianceCounts map[string]int64
}

// ActionTarget is the metadata-only scanner view of a custom action target.
type ActionTarget struct {
	ARN         string
	Name        string
	Description string
}

// Insight is the metadata-only scanner view of a custom Security Hub insight.
type Insight struct {
	ARN              string
	Name             string
	GroupByAttribute string
	ControlIDs       []string
}

// FindingCount is an aggregate Security Hub posture count derived from
// GetFindings results without carrying finding bodies.
type FindingCount struct {
	StandardID       string
	ControlID        string
	ComplianceStatus string
	SeverityLabel    string
	WorkflowStatus   string
	Count            int64
}
