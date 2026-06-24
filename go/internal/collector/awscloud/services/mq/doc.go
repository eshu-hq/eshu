// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package mq maps Amazon MQ broker and configuration metadata into AWS cloud
// collector facts. One scan slice covers both ActiveMQ and RabbitMQ engine
// types.
//
// The scanner emits reported-confidence broker and broker-configuration
// resources plus relationships for subnet, security group, KMS key,
// configuration, and CloudWatch Logs log group dependencies. Broker user
// passwords, configuration XML bodies, queue and topic message contents, and
// every broker, configuration, and user mutation API stay outside this package
// contract. Broker usernames are recorded for topology; the User Password field
// is never modeled or persisted.
package mq
