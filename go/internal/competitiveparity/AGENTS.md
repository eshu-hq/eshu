# AGENTS.md - competitiveparity

## Ownership

This package owns the offline competitive parity gate for shipped Eshu
capability surfaces. It validates captured or embedded surface inventories,
command paths, and documentation text; it does not call live API, MCP, graph,
Postgres, provider, or GitHub services.

## Rules

- Keep artifacts publish-safe. Do not include private hostnames, repository
  paths, credentials, IP addresses, or customer-specific excerpts.
- Preserve deterministic output. Sort every externally visible slice before
  rendering JSON or Markdown.
- Treat missing surfaces as failures, not warnings. The gate exists to catch
  regressions where a closed capability is no longer reachable.
- Link residual gaps to existing issues. Do not create duplicate follow-up
  issues from this package.
- Add tests before changing criteria, default expectations, or render shape.
