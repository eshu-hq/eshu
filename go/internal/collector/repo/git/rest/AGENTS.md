# AGENTS.md - rest

Scoped to `go/internal/collector/repo/git/rest`. Follow the root
[AGENTS.md](../../../../../../AGENTS.md) first.

## Read first

1. `README.md` for the flow and the constants.
2. `doc.go` for the contract.
3. `call.go` (`Do`) before changing a fault class.

## Rules

- `Do` is the one place that classifies a GitHub response. Add a fault class
  there with a test that drives it through an `httptest` server; never add a
  second classifier in the git package.
- A change to the attempt budget, the 60 s in-call cap, the 1 h hint cap, or
  the 20-cycle cap needs a measured reason and a matching update to
  `TestRetryBudgetConstantsArePinned`, `README.md`, and the evidence note
  `docs/internal/evidence/8084-github-discovery-retry.md`. There is no
  operator knob by ruling.
- Reuse `sdk.IsTransientTransportError` and `sdk.TransportFailureClass`. Only
  the HTTP/2 stream reset is added here.
- Cancellation must stay out of `*RetryableError`.
- Label sets on `eshu_dp_github_api_*` are closed: `operation` is `list` or
  `mint`; `outcome` is `success`, `retried`, or `failed`.
- Import no git collector type. `collector` is imported only for
  `collector.CycleSkip` and the `CycleSkipper` contract, which
  `*RetryableError` implements. The parent package imports this one.
- Keep files under 500 lines.
