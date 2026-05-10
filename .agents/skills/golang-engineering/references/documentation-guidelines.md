# Documentation Guidelines

Use this file when Go code changes alter behavior, setup, public contracts, or non-obvious maintenance assumptions. Treat it as a concise baseline aligned with Google's documentation best practices when the repo does not provide a clearer documentation standard.

## Core Rule

Documentation must earn its keep. Add information the code and type signatures do not already say.

## What to Document

- package purpose and boundaries
- invariants and non-obvious assumptions
- edge cases and failure modes
- exported API contracts and caller expectations
- setup, configuration, or migration steps affected by the change
- examples when they materially shorten reader confusion

## Package Docs

- Add a package comment when the package is intended for reuse, has non-obvious boundaries, or benefits from a one-sentence purpose statement.
- Explain what the package is for, not how every file works.
- Keep package comments accurate and short enough to stay maintained.

Prefer this:

```go
// Package cache provides a bounded in-memory cache for request-scoped metadata.
package cache
```

Over this:

```go
// Package cache contains cache code.
package cache
```

## Doc Comments

- Exported identifiers should be documented when they are part of the package contract or their behavior is not obvious from the signature.
- Start doc comments with the identifier name.
- Write doc comments as documentation, not placeholders.
- Avoid boilerplate that merely repeats the name and types.

Prefer this:

```go
// ParsePort parses a positive TCP port from configuration input.
// It returns an error for empty or invalid values.
func ParsePort(raw string) (int, error) { /* ... */ }
```

Over this:

```go
// ParsePort parses a port.
func ParsePort(raw string) (int, error) { /* ... */ }
```

## Inline Comments

- Use comments to explain intent, invariants, security constraints, protocol details, or surprising behavior.
- Do not narrate obvious code.

Good comment:

```go
// Keep the raw body bytes. Signature verification depends on the exact byte stream.
rawBody := payload
```

Bad comment:

```go
// Store the body in rawBody.
rawBody := payload
```

## Docs That Must Change With Code

Update nearby docs in the same change when you modify:
- exported API shape
- CLI flags or config fields
- setup steps
- error handling users must react to
- examples, snippets, or README guidance

## Accuracy Rules

- Prefer short accurate docs over long stale docs.
- Delete outdated claims instead of layering new prose on top.
- Keep examples runnable or obviously correct.
- If something was not verified, do not imply that it was.
