// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Control Tower client into the
// metadata-only Control Tower scanner interface.
//
// The adapter uses ListLandingZones, GetLandingZone, ListEnabledBaselines,
// ListEnabledControls, and ListTagsForResource to read landing-zone,
// enabled-baseline, and enabled-control control-plane metadata and resource
// tags. It never reads the landing-zone manifest body that GetLandingZone
// returns, never reads control or baseline parameter values, and intentionally
// excludes every EnableControl/DisableControl, EnableBaseline/
// ResetEnabledBaseline, CreateLandingZone, and Update/Delete mutation API, so
// the adapter cannot read a governance payload or change Control Tower state.
package awssdk
