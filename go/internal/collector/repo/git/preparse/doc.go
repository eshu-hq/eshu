// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package preparse decides, before the parse, whether a forced reconcile of a
// git repository would emit the same facts as the scope's active generation
// (#7978 follow-up, ruling a9-143), and runs that decision for the git
// collector.
//
// The daily reconcile sweep used to fetch, check out, discover, parse, and
// build the whole fact stream of every unchanged repository, and only then let
// the ingestion store compare the post-parse freshness hint and re-project the
// active generation. Identity is a cheap stand-in for that hint: the content
// version, the commit, the default branch, and every configuration input that
// changes emitted facts (Config). A healthy snapshot is a deterministic
// function of those inputs, so an equal identity means an equal hint, and the
// parse can be skipped. A snapshot that degraded (a SCIP fallback or a file it
// could not read) is not; the git collector stamps no identity on it, so the
// next sweep parses.
//
// The pure part: Identity, ConfigDigest, and Decide do no I/O, read no clock,
// and run no git, so they are table-testable. Decide applies the rules: only
// interval_elapsed and graph_dirty may skip; the active generation must be a
// full at the remote head with an equal stored identity; repositories with
// submodules and dirty checkouts always parse; and a valve forces a parse at
// least every MaxSkips intervals, bounding any input the identity does not
// cover. Every other outcome parses as before. FoldedEnvKeys and
// NonOutputEnvKeys classify every env key read by the git collector, discovery,
// and the parsers; TestEveryEnvKeyIsClassified fails on a new key until someone
// decides whether it changes output.
//
// The side effects live in Run. It calls Decide; on a skip it asks the store,
// through Reprojector, to re-project the active generation, and on a proven
// first-sweep stamp (#8095) to stamp its empty stored identity instead —
// never through the identity-equality path, whose fence would refuse it. It
// maps the store's answer to the outcome (reconcile_reprojected is skipped,
// or stays stamped; reconcile_reprojection_pending is reprojection_pending,
// reconcile_superseded is reprojection_superseded and parses, anything else is
// read_error and parses). It records the decision through Recorder: one increment of
// eshu_dp_collector_reconcile_parse_skip_decisions_total{outcome}, the span
// event git.reconcile.parse_skip_decision on the caller's span, and the INFO
// git_reconcile_parse_skipped or WARN git_reconcile_parse_skip_refused log.
// The git collector supplies the inputs and the tree check.
package preparse
