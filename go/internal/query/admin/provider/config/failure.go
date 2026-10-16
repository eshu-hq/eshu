// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package config

// Fixed client-facing messages for admin provider-config route failures
// (#7674). Each failing step answers exactly its constant through
// tracing.WriteServerFailure: 500 with the error recorded on the request span,
// or 499 with only the client-cancel event when the caller canceled. The store
// or tester error never reaches the response body.
const (
	providerConfigListFailedMessage         = "failed to list provider configs"
	providerConfigReadFailedMessage         = "failed to read provider config"
	providerConfigRevisionListFailedMessage = "failed to list provider config revisions"

	providerConfigIDFailedMessage         = "failed to generate provider config id"
	providerConfigRevisionIDFailedMessage = "failed to generate provider config revision id"
	providerConfigWriteFailedMessage      = "failed to write provider config"
	connectionTestRunFailedMessage        = "connection test failed to run"
)
