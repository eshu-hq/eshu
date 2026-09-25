// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package appconfig maps AWS AppConfig application, environment, configuration
// profile, and deployment strategy metadata into AWS cloud collector facts.
//
// The scanner emits reported-confidence resources for AppConfig applications,
// environments, configuration profiles, and deployment strategies plus
// relationships for environment-in-application, profile-in-application,
// environment-to-CloudWatch-alarm (deployment monitor), and
// environment-to-IAM-role (the monitor alarm role) evidence. Configuration
// content, hosted configuration version bodies, freeform/feature-flag values,
// and any mutation or deployment-start API stay outside this package contract:
// the scanner is metadata-only.
package appconfig
