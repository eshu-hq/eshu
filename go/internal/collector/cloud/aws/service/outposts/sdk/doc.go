// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Outposts client into the
// metadata-only Outposts scanner interface.
//
// The adapter uses ListOutposts, GetOutpost, ListSites, GetSite, ListAssets,
// and ListTagsForResource to read Outposts outpost, site, and asset
// control-plane metadata and resource tags. It intentionally excludes
// GetSiteAddress and every order, billing, connection, catalog, pricing,
// renewal, capacity-task, and instance-type read, and all Create/Update/Delete/
// Cancel mutation APIs, so the adapter cannot read physical site street
// addresses, shipping or contact details, or rack physical-property logistics,
// and cannot mutate Outposts state.
package awssdk
