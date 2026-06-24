// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package appstream maps Amazon AppStream 2.0 fleet, stack, image builder, and
// image metadata into AWS cloud collector facts.
//
// The scanner emits reported-confidence resources for AppStream fleets, stacks,
// image builders, and images plus relationships for fleet and image-builder VPC
// subnet, security group, IAM role, and source-image dependencies, fleet-to-
// stack associations, and the stack S3 bucket dependencies AppStream reports for
// persistent application settings and home-folders storage connectors. Streaming
// sessions, user data, session scripts, streaming-URL credentials, and any
// AppStream mutation API stay outside this package contract: the scanner is
// metadata-only.
package appstream
