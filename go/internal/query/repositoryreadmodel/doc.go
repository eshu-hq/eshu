// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package repositoryreadmodel holds the repository identity and page-shaping
// reads for the repository handler family (Issue #6060, lane B): repository
// ref resolution, repository name lookup, and bounded list-page shaping. It
// imports only the standard library, net/http, and querycontract, so both
// the repository family and the staying root package consume it without an
// import cycle.
package repositoryreadmodel
