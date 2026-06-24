// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Amazon MQ client into the
// metadata-only MQ scanner interface for both ActiveMQ and RabbitMQ brokers.
//
// The adapter uses ListBrokers, DescribeBroker, and ListConfigurations. It
// intentionally excludes CreateBroker, UpdateBroker, DeleteBroker,
// RebootBroker, CreateConfiguration, UpdateConfiguration, DeleteConfiguration,
// CreateUser, UpdateUser, DeleteUser, CreateTag, DeleteTag, DescribeUser
// (which returns the password-bearing User resource), and
// DescribeConfigurationRevision (which would expose the configuration XML
// body). DescribeBroker returns broker usernames (UserSummary) but no
// passwords; the adapter records usernames only.
package awssdk
