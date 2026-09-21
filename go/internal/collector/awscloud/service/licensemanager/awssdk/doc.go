// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 License Manager client into the
// metadata-only License Manager scanner interface.
//
// The adapter uses ListLicenseConfigurations,
// ListAssociationsForLicenseConfiguration, and ListTagsForResource to read
// License Manager license-configuration control-plane metadata, resource
// associations, and resource tags. It intentionally excludes GetLicense,
// CheckoutLicense, CheckInLicense, GetAccessToken, every grant API, and all
// Create/Update/Delete mutation APIs, so the adapter cannot grant, check out,
// or mutate a license or read a license entitlement token or usage record.
package awssdk
