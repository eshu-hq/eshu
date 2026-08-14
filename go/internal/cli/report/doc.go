// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package report builds and checks wrong-answer report bundles for `eshu
// report capture` and `eshu report validate`: issuing the query a reporter ran,
// composing the answer into a wrong_answer_report.v1 bundle through
// internal/reportbundle, writing that bundle owner-only, and validating a
// bundle somebody sent back.
//
// CaptureBundle is the entry point. It screens the reporter's target for an
// embedded credential, normalizes the method, decodes the raw --params JSON,
// issues the request through the caller-supplied EnvelopeClient, and returns
// the finished reportbundle.Bundle together with the exact bytes to write.
// WriteBundle, ReadBundleInput and ValidateBundle cover the file and validation
// halves. IncludePayloadsWarning is the text a caller prints when the reporter
// asks for raw payloads.
//
// # Redaction
//
// This package owns exactly two credential rules; internal/reportbundle owns
// every field-by-field rule inside the bundle itself.
//
//   - A target (--endpoint or --tool) carrying URL userinfo is REFUSED with a
//     TargetCredentialError before any request goes out, as is a target net/url
//     cannot parse. Userinfo sits before the "?", so reportbundle's rules —
//     which all match an object key name — have no key to match and once let a
//     password reach query.target of a bundle stamped public and passed.
//   - A transport failure has the request URL replaced by the endpoint path,
//     and that path has its own userinfo stripped.
//
// It does NOT cover: a credential written under a benign parameter name
// (reportbundle's own documented limit); a full URL pasted inside a path
// segment, which is not an authority component and so carries no userinfo as
// far as net/url is concerned; or a server that echoes the request URL back
// inside a 4xx/5xx response body, which arrives as go/cmd/eshu's apiHTTPError
// and cannot be read from internal/cli. The request itself is deliberately
// unredacted — the answer under investigation is the one the reporter's own
// credential returns.
//
// # Boundary
//
// The package reads no cobra flags — spf13/cobra is not in its dependency
// graph — resolves no Eshu config or credential from the process environment,
// never calls os.Exit, and prints only through io.Writer parameters callers
// supply. It reads and writes local files through explicit path parameters,
// which is mechanical input handling rather than process wiring.
// go/cmd/eshu's report_cmd.go is the thin cobra wrapper that resolves flags,
// streams and exit codes; go/cmd/eshu is package main, so nothing can import
// it and anything reading process state has to stay there.
package report
