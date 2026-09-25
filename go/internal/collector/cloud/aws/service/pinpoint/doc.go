// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package pinpoint maps Amazon Pinpoint application, segment, and
// channel-settings metadata into AWS cloud collector facts.
//
// The scanner emits reported-confidence resources for Pinpoint applications,
// segments, and channels plus relationships for application-to-segment,
// channel-in-application, and (for the email channel) channel-to-SES-identity
// and channel-to-SES-configuration-set evidence. Endpoint records, addresses,
// message and template content, segment targeting criteria values, the import
// S3 URL, the email from-address, and channel credentials stay outside this
// package contract: the scanner is metadata-only and never sends a message or
// mutates Pinpoint state.
package pinpoint
