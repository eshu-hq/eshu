// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

// AcceptedProviderConfigKinds is the canonical set of provider_kind values
// the admin provider-config write path accepts ("oidc", "saml", "github").
//
// It lives here rather than beside either consumer because the #6060 split
// put the two lockstep partners in different test packages that cannot
// share declarations: the builder-acceptance proof lives in
// internal/query/admin/provider/config (it calls the unexported builder
// directly), and the OpenAPI-enum proof lives in the external admin_test
// package (it reads the root's OpenAPISpec, which an internal admin test
// cannot import without a cycle). Two copies of the list would let the
// partners drift apart silently, which is exactly the F-5 (issue #5166) gap
// the lockstep guards. Add a new kind here in the same change that adds its
// builder case and its spec enums.
var AcceptedProviderConfigKinds = []string{"oidc", "saml", "github"}
