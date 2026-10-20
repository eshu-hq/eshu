// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts AWS SDK for Go v2 SNS calls into scanner-owned topic
// metadata.
//
// The adapter only calls ListTopics, GetTopicAttributes, ListTagsForResource,
// and ListSubscriptionsByTopic. It must not call Publish, Subscribe,
// Unsubscribe, SetTopicAttributes, or persist raw non-ARN subscription
// endpoints.
package awssdk
