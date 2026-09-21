// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Service Quotas client into the
// metadata-only Service Quotas scanner interface.
//
// The adapter uses ListServices to discover the services visible to the claim,
// ListServiceQuotas to read each service's applied quota values, and
// ListAWSDefaultServiceQuotas to read each service's AWS-published defaults,
// joining them by quota code so the scanner can mark approved overrides. It
// intentionally excludes RequestServiceQuotaIncrease, every quota-increase
// template association, the requested-change-history reads, and all Put/Delete
// mutation APIs, so the adapter cannot change quota state. The accepted surface
// is List-only by construction, enforced by exclusion_test.
package awssdk
