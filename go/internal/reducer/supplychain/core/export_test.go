// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

// OSPackageAdvisoryFactLoaderForTest exposes the unexported reader interface the
// handler reaches through a runtime type assertion, so core_test can assert at
// compile time that the production reader still satisfies it.
type OSPackageAdvisoryFactLoaderForTest = osPackageAdvisoryFactLoader
