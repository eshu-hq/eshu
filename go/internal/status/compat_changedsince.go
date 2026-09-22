// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

// This file is the status root's compatibility surface for the changedsince family
// that moved to [changedsince] (issue #6775). It carries no behavior change: every
// alias names the same type and every constant the same value, so the packages
// importing internal/status keep compiling unchanged. Each entry is deleted
// once its last caller has moved to the leaf; see the importer-migration child
// issue. A later changedsince move adds a stanza here and never creates a second
// compat file for this family.

import "github.com/eshu-hq/eshu/go/internal/status/changedsince"

// Changed-since delta sections.
//
// Deprecated: use the [changedsince] names.
type (
	ChangedSinceClassification = changedsince.Classification
	ChangedSinceCategory       = changedsince.Category
	ChangedSinceCategoryDelta  = changedsince.CategoryDelta
	ChangedSinceFilter         = changedsince.Filter
	ChangedSinceCounts         = changedsince.Counts
	ChangedSinceSample         = changedsince.Sample
	ChangedSinceSummary        = changedsince.Summary
	ServiceChangedSinceFilter  = changedsince.ServiceFilter
	ServiceChangedSinceSummary = changedsince.ServiceSummary
)

// Changed-since classifications, categories, reasons and sample bounds.
//
// Deprecated: use the [changedsince] constants.
const (
	ChangedSinceAdded      = changedsince.Added
	ChangedSinceUpdated    = changedsince.Updated
	ChangedSinceUnchanged  = changedsince.Unchanged
	ChangedSinceRetired    = changedsince.Retired
	ChangedSinceSuperseded = changedsince.Superseded

	ChangedSinceCategoryFiles           = changedsince.CategoryFiles
	ChangedSinceCategoryContentEntities = changedsince.CategoryContentEntities
	ChangedSinceCategoryFacts           = changedsince.CategoryFacts
	ChangedSinceCategoryOwnership       = changedsince.CategoryOwnership
	ChangedSinceCategoryDeployment      = changedsince.CategoryDeployment
	ChangedSinceCategoryRuntime         = changedsince.CategoryRuntime
	ChangedSinceCategoryDependencies    = changedsince.CategoryDependencies
	ChangedSinceCategoryDocs            = changedsince.CategoryDocs
	ChangedSinceCategoryIncidents       = changedsince.CategoryIncidents
	ChangedSinceCategoryVulnerabilities = changedsince.CategoryVulnerabilities

	ChangedSinceUnavailableRetentionExpired = changedsince.UnavailableRetentionExpired

	MaxChangedSinceSampleLimit     = changedsince.MaxSampleLimit
	DefaultChangedSinceSampleLimit = changedsince.DefaultSampleLimit
)
