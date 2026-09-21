// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 AppStream 2.0 client into the
// metadata-only AppStream scanner interface.
//
// The adapter uses DescribeFleets, DescribeStacks, DescribeImageBuilders,
// DescribeImages, ListAssociatedStacks, and ListTagsForResource to read
// AppStream control-plane metadata and resource tags. It intentionally excludes
// every Create/Update/Delete/Start/Stop mutation, CreateStreamingURL (which
// mints a session credential), and the session/user describe APIs
// (DescribeSessions, DescribeUsers, DescribeUserStackAssociations), so the
// adapter cannot mutate AppStream state or read streaming-session, user, or
// credential content.
package awssdk
