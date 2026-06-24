// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 SES v2 client into the
// metadata-only SES scanner interface.
//
// The adapter uses ListEmailIdentities, GetEmailIdentity, ListConfigurationSets,
// GetConfigurationSet, GetConfigurationSetEventDestinations, and
// ListDedicatedIpPools to read SES email-identity, configuration-set,
// event-destination, and dedicated-IP-pool control-plane metadata. It
// intentionally excludes every send API (SendEmail, SendBulkEmail,
// SendCustomVerificationEmail, SendRawEmail), every template, contact, and
// suppression read, and all Create/Update/Delete/Put mutation APIs, so the
// adapter cannot send email or write SES state. It never persists the DKIM
// signing tokens, identity policy documents, or any signing-key material the
// SES API also returns.
package awssdk
