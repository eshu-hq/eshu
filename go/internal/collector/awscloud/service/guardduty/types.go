// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package guardduty

import "context"

// Client lists GuardDuty metadata for one claimed account and region.
type Client interface {
	ListDetectors(context.Context) ([]Detector, error)
}

// Detector is the metadata-only scanner view of a GuardDuty detector.
type Detector struct {
	ID                         string
	Status                     string
	FindingPublishingFrequency string
	CreatedAt                  string
	UpdatedAt                  string
	Tags                       map[string]string
	Features                   []FeatureConfiguration
	FindingCountsBySeverity    map[string]int64
	FindingCountsByType        map[string]int64
	Members                    []MemberAccount
	Filters                    []FilterSummary
	PublishingDestinations     []PublishingDestination
	ThreatIntelSets            []ThreatIntelSet
	IPSets                     []IPSet
}

// FeatureConfiguration is a safe GuardDuty detector feature summary.
type FeatureConfiguration struct {
	Name                    string
	Status                  string
	UpdatedAt               int64
	AdditionalConfiguration []FeatureConfiguration
}

// MemberAccount is a safe GuardDuty administrator/member account summary.
type MemberAccount struct {
	AccountID          string
	AdministratorID    string
	DetectorID         string
	RelationshipStatus string
	InvitedAt          string
	UpdatedAt          string
}

// FilterSummary is a GuardDuty filter identity without criteria expressions.
type FilterSummary struct {
	Name string
}

// PublishingDestination is safe GuardDuty finding export metadata.
type PublishingDestination struct {
	ID              string
	DestinationType string
	Status          string
	DestinationARN  string
	Tags            map[string]string
}

// ThreatIntelSet is a GuardDuty threat intel set summary without list contents.
type ThreatIntelSet struct {
	ID          string
	Name        string
	Format      string
	Status      string
	LocationARN string
	Tags        map[string]string
}

// IPSet is a GuardDuty IP set summary without list contents.
type IPSet struct {
	ID          string
	Name        string
	Format      string
	Status      string
	LocationARN string
	Tags        map[string]string
}
