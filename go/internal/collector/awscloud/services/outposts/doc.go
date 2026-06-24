// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package outposts maps AWS Outposts outpost, site, and asset metadata into AWS
// cloud collector facts.
//
// The scanner emits reported-confidence resources for outposts, sites, and
// rack/server assets plus the outpost-in-site and asset-in-outpost membership
// relationships. It is metadata-only: it reads control-plane describe/list APIs
// (ListOutposts, GetOutpost, ListSites, GetSite, ListAssets,
// ListTagsForResource) and never mutates Outposts state. Physical site street
// addresses, shipping or contact details, free-form site notes, and rack
// physical-property logistics stay outside this package contract and are never
// copied into a fact payload.
package outposts
