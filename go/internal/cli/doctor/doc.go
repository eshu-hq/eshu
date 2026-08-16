// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package doctor owns the report behind `eshu doctor`: the local-environment
// check an operator runs first when a stack will not come up.
//
// # What it reports
//
// Six things, in the order an operator needs them: the config directory, the
// persisted settings file, each Eshu service binary on PATH, the API's /health
// endpoint, the graph Bolt URI, and the Postgres DSN. Every check is advisory.
// Run returns nil even when everything is broken, because an operator running
// doctor already knows something is wrong and wants the whole picture rather
// than the first failure.
//
// # No credential reaches the report
//
// Doctor output is the first thing pasted into a bug report, so the report is
// a redaction surface even though it reads like a status list.
//
// The claim is about credentials specifically, and it is deliberately not
// "nothing sensitive is printed". The config directory and settings-file paths
// ARE printed verbatim, and an absolute home path names its user. That is a
// considered trade: doctor exists to tell an operator which path it looked at,
// and "[!!] Config directory missing: .../.eshu" is not actionable. Callers
// who need the path elided have evidredact.Path; this report does not use it.
//
// The Bolt URI carries its password in userinfo -- bolt://neo4j:PASS@host:7687
// is the canonical form -- and it is written through evidredact.Endpoint, which
// replaces userinfo and strips the query and fragment while keeping the host an
// operator needs in order to recognise which backend was configured. The API
// base URL goes through the same call, because an operator-configured URL can
// carry a token in its query string. The Postgres DSN is reported by presence
// only: it is credentials end to end, with no host-shaped remainder worth
// showing.
//
// doctor_redaction_test.go is the screen, and it plants its sentinel inside a
// value rather than at a token boundary so a check that only matches whole
// fields cannot pass by accident.
//
// # Ownership boundary
//
// This package holds no process wiring. It reads no cobra flags, no
// environment, and no process streams; it never exits; and it writes only to
// the io.Writer its caller supplies. The filesystem and PATH lookups it does
// perform go through Deps, so a test can describe a broken machine without
// being run on one.
//
// That claim is enforced rather than asserted: TestPackageStaysProcessNeutral
// parses this directory and fails on a cobra import or on any process-bound
// os/fmt selector. A dependency scan cannot do that job here, because os and
// fmt are legitimate imports for the Deps seam and for formatting.
//
// go/cmd/eshu is package main, so nothing can import it. Resolving NEO4J_URI
// from the environment and then the settings file, and resolving the API base
// URL, both stay in the cobra wrapper doctor.go, which passes the results in
// as plain values.
package doctor
