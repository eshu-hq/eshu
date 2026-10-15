// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package failure is the shared failure-case harness for query routes that
// answer a failed backend read through tracing.WriteServerFailure and
// querycontract.WriteGraphReadError (#7674).
//
// A test lists its routes in a Suite and calls Run. Each Route names the
// request, the fixed message the route answers with, and a constructor that
// builds the handler whose read under test fails with a given error. Run
// drives every route through Cases, one per Kind:
//
//   - Fault: an error quoting Canary answers 500 with only the fixed
//     message, and the span records one exception and status Error.
//   - ClientCancel: a canceled request context and an error wrapping
//     context.Canceled answer 499 with the fixed message; the span status
//     stays Unset with only the attribute-free tracing.ClientCanceledEvent.
//   - InnerCancel: the same error on a live request is a server fault.
//   - ReaderFence: db.ErrReaderStale answers 503 backend_unavailable with
//     Retry-After.
//
// No case may put any Canary token in the body. Each condition is an
// Assertion constant, listed by Assertions and evaluated independently.
// Evaluate returns every Violation for one case, and Check joins them into an
// error instead of failing the test, which is how this package's own tests
// prove each assertion rejects a route wrong in that one way. Serve and Message are
// the building blocks for a route test that needs one request and its body
// text, such as a 400 or 404 sentinel check.
//
// The parent testutil contract applies: helpers live in ordinary non-test
// files, nothing here is production behavior, and no non-test file under
// internal/query may import this package (internal/queryplan's production
// import guard matches the testutil path element).
package failure
