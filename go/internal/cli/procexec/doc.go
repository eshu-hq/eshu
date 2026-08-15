// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package procexec holds the shared "resolve the eshu binary, build its
// environment, hand the process over to it" seam used by go/cmd/eshu.
//
// Four unrelated command families re-exec a binary: `eshu watch` (basic.go),
// `eshu scan` (scan.go), `eshu vuln scan --local` (vuln_scan_local.go), and
// `eshu graph start` (graph.go), plus the local owner and MCP start paths in
// service.go and local_graph_process.go. They shared these symbols through
// go/cmd/eshu's package scope, which coupled every one of them to the
// local_host/local_graph supervisor cluster for nothing more than an
// os.Environ call. This package is that shared machinery on its own.
//
// # The seams and why they are variables
//
// Executable, Getwd, LookPath, Exec, and Environ are package-level variables
// holding function values, not functions. That is deliberate and it is the
// package's whole reason to exist.
//
// Exec is the one that forces it. It calls syscall.Exec, which REPLACES THE
// RUNNING PROCESS IMAGE: on success the call does not return, the calling
// program ceases to exist, no deferred function runs, and no buffered output
// is flushed. A test that reached a real syscall.Exec would not fail -- the
// test binary would be gone. Every code path that re-execs is therefore
// untestable in-process unless the call goes through something a test can
// reassign. The other four reach host state (the running binary's path, the
// working directory, PATH, the process environment) that a test cannot
// arrange either, and go/cmd/eshu's tests substitute all five.
//
// These are a test seam, not configuration. Production code assigns to them
// nowhere. A test that assigns one must restore the original and must not run
// in parallel with another test touching the same seam, because they are
// process-global state.
//
// syscall.Exec compiles on every GOOS, windows included, but the windows
// implementation is a stub returning EWINDOWS. A green GOOS=windows build is
// not evidence that process replacement works there.
//
// # The two pure helpers
//
// CleanExecutableArg0 reduces a resolved binary path to the argv[0] a child
// should see. MergeEnvironment layers a name->value override map onto a
// name=value slice, splitting each entry on its FIRST '=' so values may
// contain more of them, dropping an entry that has no '=' at all, and letting
// the last occurrence of a repeated name win. Its result comes out of a map,
// so entry order is unspecified. Neither is a variable: nothing overrides
// them, and both are directly testable.
//
// # Boundary
//
// This package reads no cobra flags, prints nothing, and maps nothing to an
// exit code -- go/cmd/eshu is `package main`, so nothing can import it, and
// any symbol that touches a flag or an exit code has to stay in the wrapper
// there. It imports only the standard library (`go list -deps` reports no
// non-stdlib dependency and no spf13/cobra). Environ and LookPath read the
// process environment and PATH by definition; that is the package's job, not
// a leak of process wiring.
package procexec
