// inspectionRequest builds a Request from a fetcher's (input, init) purely so a
// test can read its url and headers. jsdom's Request constructor rejects any
// real AbortSignal — it instanceof-checks against an internal AbortSignal class
// that differs from the exposed global — so the signal must be stripped before
// construction. EshuApiClient now attaches a per-request timeout AbortSignal in
// production (issue #1680); that behavior is asserted directly against the
// recorded init in client.test.ts, not through this inspection Request.
export function inspectionRequest(
  input: RequestInfo | URL,
  init?: RequestInit
): Request {
  return new Request(input, { ...init, signal: undefined });
}
