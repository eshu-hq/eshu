// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package runtimebind registers the Elastic Beanstalk scanner with the
// awsruntime registry.
//
// The package has no exported surface. Importing it for its init side effect
// adds the Elastic Beanstalk scanner builder to the registry so
// DefaultScannerFactory can resolve service_kind "elasticbeanstalk" without a
// central switch. The registration declares RequiresRedactionKey, so the
// runtime command derives the ESHU_AWS_REDACTION_KEY pre-flight requirement
// from the registry rather than a hand-maintained service switch. Production
// callers pull every service binding through
// internal/collector/awscloud/awsruntime/bindings.
package runtimebind
