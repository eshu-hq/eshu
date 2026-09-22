// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

// This file is the status root's compatibility surface for the tfstate family
// that moved to [tfstate] (issue #6775). It carries no behavior change: every
// alias names the same type and every constant the same value, so the packages
// importing internal/status keep compiling unchanged. Each entry is deleted
// once its last caller has moved to the leaf; see the importer-migration child
// issue. A later tfstate move adds a stanza here and never creates a second
// compat file for this family.

import "github.com/eshu-hq/eshu/go/internal/status/tfstate"

// Terraform state admin status section.
//
// Deprecated: use the [tfstate] names.
type (
	TerraformStateReport         = tfstate.Report
	TerraformStateLocatorSerial  = tfstate.LocatorSerial
	TerraformStateLocatorWarning = tfstate.LocatorWarning
	TerraformStateWarningSummary = tfstate.WarningSummary
)

// MaxTerraformStateRecentWarnings bounds recent warnings per locator.
//
// Deprecated: use [tfstate.MaxRecentWarnings].
const MaxTerraformStateRecentWarnings = tfstate.MaxRecentWarnings
