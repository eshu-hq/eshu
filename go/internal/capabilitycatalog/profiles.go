// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capabilitycatalog

// Profile is a runtime profile id. The catalog mirrors the profile vocabulary
// defined by the capability matrix and the query runtime
// (go/internal/query/contract.go) without importing the query package, which
// keeps the catalog free of HTTP and graph dependencies.
type Profile string

const (
	// ProfileLocalLightweight is the local host profile without a graph sidecar.
	ProfileLocalLightweight Profile = "local_lightweight"
	// ProfileLocalAuthoritative is the local host profile with a graph sidecar.
	ProfileLocalAuthoritative Profile = "local_authoritative"
	// ProfileLocalFullStack is the local full-stack profile.
	ProfileLocalFullStack Profile = "local_full_stack"
	// ProfileProduction is the deployed-services profile.
	ProfileProduction Profile = "production"
)
