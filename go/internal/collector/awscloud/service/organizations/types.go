// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package organizations

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client returns an AWS Organizations metadata snapshot for one claimed
// management or delegated-administrator account.
type Client interface {
	Snapshot(context.Context) (Snapshot, error)
}

// Snapshot is the metadata-only scanner view of AWS Organizations.
type Snapshot struct {
	Organization            Organization
	Roots                   []Root
	OrganizationalUnits     []OrganizationalUnit
	Accounts                []Account
	Policies                []Policy
	DelegatedAdministrators []DelegatedAdministrator
	Warnings                []awscloud.WarningObservation
}

// Organization carries the organization-wide metadata safe to attach to root
// facts.
type Organization struct {
	ARN               string
	ID                string
	ManagementAccount string
	FeatureSet        string
}

// Root is the metadata-only scanner view of an Organizations root.
type Root struct {
	ARN         string
	ID          string
	Name        string
	PolicyTypes []PolicyTypeSummary
	Tags        map[string]string
}

// PolicyTypeSummary records the status of one policy family on a root.
type PolicyTypeSummary struct {
	Type   string
	Status string
}

// OrganizationalUnit is the metadata-only scanner view of an OU.
type OrganizationalUnit struct {
	ARN      string
	ID       string
	Name     string
	ParentID string
	Tags     map[string]string
}

// Account is the metadata-only scanner view of an Organizations account.
type Account struct {
	ARN       string
	Email     string
	ID        string
	JoinedAt  time.Time
	JoinedVia string
	Name      string
	ParentID  string
	State     string
	Status    string
	Tags      map[string]string
}

// Policy is an Organizations policy summary plus target bindings. It never
// carries the policy body.
type Policy struct {
	ARN         string
	AWSManaged  bool
	Description string
	ID          string
	Name        string
	Type        string
	Targets     []PolicyTarget
	Tags        map[string]string
}

// PolicyTarget is one root, OU, or account binding reported by Organizations.
type PolicyTarget struct {
	ARN  string
	ID   string
	Name string
	Type string
}

// DelegatedAdministrator is one account/service-principal delegation binding.
type DelegatedAdministrator struct {
	AccountARN          string
	AccountEmail        string
	AccountID           string
	AccountName         string
	DelegationEnabledAt time.Time
	JoinedAt            time.Time
	JoinedVia           string
	ServicePrincipal    string
	State               string
	Status              string
}
