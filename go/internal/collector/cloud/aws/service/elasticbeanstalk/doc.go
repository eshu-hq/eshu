// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package elasticbeanstalk maps AWS Elastic Beanstalk observations into AWS
// cloud fact envelopes.
//
// The package owns scanner-level fact selection for applications, environments,
// application versions, and the relationships Elastic Beanstalk reports
// (environment-to-application, environment-to-VPC, environment-to-IAM
// instance-profile/service-role, environment-to-load-balancer,
// environment-to-Auto-Scaling-group, environment-to-launch-template, and
// environment-to-application-version). It is metadata-only: every environment
// option-setting value is replaced with a redaction marker before persistence,
// and the scanner-owned types carry no field for source bundle object contents
// or environment-info bundles. AWS SDK pagination, credentials, persistence,
// graph projection, and reducer-owned correlation live outside this package.
package elasticbeanstalk
