// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package runtimebind registers the SageMaker scanner with the awsruntime
// registry.
//
// The package has no exported surface. Importing it for its init side effect
// adds the SageMaker scanner builder to the registry so DefaultScannerFactory
// can resolve service_kind "sagemaker" without a central switch. The builder
// takes no optional dependency: SageMaker is metadata-only and needs no
// redaction key or pagination checkpoint store. Production callers pull every
// service binding through internal/collector/awscloud/awsruntime/bindings.
package runtimebind
