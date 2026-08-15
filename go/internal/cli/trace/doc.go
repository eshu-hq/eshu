// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package trace builds and renders the `eshu trace service` view of how a
// service gets from source code to runtime.
//
// It owns the request (FetchServiceStory), the envelope types the response
// decodes into (ServiceEnvelope, ServiceError), the two renderings an operator
// sees (RenderServiceSummary and RenderServiceError), and the two envelope
// readings the command branches on (ServiceFreshnessState, ServiceStatus).
//
// It owns none of the command plumbing. Cobra flags, the concrete API client,
// process environment, and the mapping from a failed trace to a process exit
// code all stay in go/cmd/eshu, which is package main and therefore cannot be
// imported from here. Nothing in this package imports cobra, and the transport
// it needs is the one-method EnvelopeFetcher declared where it is consumed.
//
// Two contracts constrain edits. Renderer output is operator-facing and pinned
// byte for byte by tests in this package and in go/cmd/eshu, so a formatting
// change is a behavior change. The envelope readers in value.go are one of four
// copies of a set that cannot be shared across a package main boundary;
// TestEnvelopeReaderParity in go/cmd/eshu fails when the copies drift apart.
package trace
