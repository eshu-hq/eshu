// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Verified Permissions client into
// the metadata-only Verified Permissions scanner interface.
//
// The adapter uses ListPolicyStores, GetPolicyStore, ListPolicies, and
// ListIdentitySources to read policy store, policy, and identity source
// control-plane metadata. It intentionally excludes GetPolicy (the Cedar policy
// statement body), GetSchema (the schema body), GetPolicyTemplate (the template
// body), IsAuthorized and BatchIsAuthorized (authorization evaluation), and
// every Create/Update/Delete/Put mutation API, so the adapter cannot read Cedar
// source, schema bodies, policy template bodies, or authorization payloads, and
// cannot mutate Verified Permissions state.
package awssdk
