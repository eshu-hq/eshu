// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package contentreader holds the shared fake database/sql driver for
// content-read tests, peeled out of querytestutil/content for #6818 move 5.
//
// A test queues ReaderQueryResult values through OpenReaderTestDB; each query
// takes the head of the queue, runs that result's SQL-text and bind-value
// assertions, and answers with its rows. Incidental reads a handler issues on
// the way (a readiness probe, a language rollup) get an empty answer shaped by
// the Reader*Columns helpers and leave the queue alone. An empty queue with no
// matching default is an error, not an empty answer.
//
// ReaderQueryContainsInOrder and ReaderCheckArgs are the driver's two
// assertions, exported for a test that holds a recorded query rather than a
// queued result.
//
// This package is intended for tests only. The helpers live in ordinary
// (non-_test.go) files on purpose: a symbol declared in a _test.go file is not
// part of the importable package, so other packages' tests could not reach the
// driver at all. No production file may import this package; a fake answers
// from values a test installs, so a production caller reaching one gets
// whatever the zero value returns, a silent wrong answer rather than a
// failure.
package contentreader
