// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Pinpoint client into the
// metadata-only Pinpoint scanner interface.
//
// The adapter uses GetApps and GetSegments (paged) and GetChannels plus
// GetEmailChannel to read Pinpoint application, segment, and channel-settings
// control-plane metadata. It intentionally excludes SendMessages and every
// other send API, GetEndpoint/GetUserEndpoints and every endpoint or export
// read, message/template content reads, and all Create/Update/Delete mutation
// APIs, so the adapter cannot read endpoint records, addresses, or message
// content and cannot mutate Pinpoint state. The email from-address is never
// copied off the GetEmailChannel response; only the SES configuration-set and
// identity references are kept.
package awssdk
