// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package accessanalyzer

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client lists Access Analyzer metadata for one claimed account and region.
type Client interface {
	ListAnalyzers(context.Context) ([]Analyzer, error)
}

// Analyzer is the metadata-only scanner view of an IAM Access Analyzer analyzer.
type Analyzer struct {
	ARN                    string
	Name                   string
	Type                   string
	Status                 string
	CreatedAt              time.Time
	LastResourceAnalyzed   string
	LastResourceAnalyzedAt time.Time
	Tags                   map[string]string
	ArchiveRules           []ArchiveRule
	FindingCounts          []FindingCount
	UnusedAccessSummaries  []UnusedAccessSummary
	Warnings               []awscloud.WarningObservation
}

// ArchiveRule is safe archive-rule metadata. Filter criteria are intentionally
// not represented because they encode security-team triage rules.
type ArchiveRule struct {
	Name        string
	AnalyzerARN string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// FindingCount is an aggregate Access Analyzer finding count bucket.
type FindingCount struct {
	Status       string
	ResourceType string
	Count        int64
}

// UnusedAccessSummary is the per-resource unused-access finding summary Eshu
// persists. Per-action unused-access details are intentionally omitted.
type UnusedAccessSummary struct {
	FindingID            string
	FindingType          string
	ResourceID           string
	ResourceOwnerAccount string
	ResourceType         string
	Status               string
	LastAccessedAt       time.Time
	AnalyzedAt           time.Time
	UpdatedAt            time.Time
}
