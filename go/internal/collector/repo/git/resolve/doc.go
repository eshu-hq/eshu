// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package resolve owns the per-host address cache the git collector
// passes to git as http.curloptResolve (#8092).
//
// Every git fetch runs as a new process, so libcurl's DNS cache never
// survives between fetches and one unanswered lookup stalls the serial sync
// loop. This package resolves each provider host once with Go's resolver,
// keeps every A and AAAA answer, and hands the caller the -c option that
// pins those addresses for clone, fetch, and ls-remote. Certificate and
// host name checks are unchanged: curl still verifies against the host
// name, it only skips its own lookup.
//
// One receiver goroutine runs every lookup. Requests are single-flight per
// host with a minimum gap, so N commands failing at once cause exactly one
// lookup. A failed or empty refresh keeps the last good set; an empty set
// is never written. A slow ticker refreshes each known host once per
// interval through the same path, so a provider address rotation costs at
// most one command deadline instead of one stall per fetch. The first
// command for a host waits at most the lookup deadline and then runs
// without the pin, counted as a cache_empty fallback.
//
// The cache never pins when a proxy is configured (curl resolves the
// proxy's name, not the remote's), when the kill switch
// ESHU_GIT_RESOLVE_CACHE is off, or for a host that is already an IP
// literal. SSH remotes never reach this package: the option is a libcurl
// setting and an SSH remote does its own lookup.
//
// Cache.RequestRefresh satisfies the deadline package's RefreshRequester
// hook structurally, so this leaf imports neither git nor deadline.
package resolve
