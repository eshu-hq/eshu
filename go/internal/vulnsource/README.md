# Vulnerability Source State

This package defines the shared, durable state contract for bounded
vulnerability intelligence source targets. It deliberately carries no storage
or runtime dependencies.

The state distinguishes configured source targets that are pending, fresh,
stale, rate limited, failed, or partial. Collectors populate the contract from
claimed source attempts, Postgres persists it with keyed upserts, and status/API
surfaces render it so empty result sets are not confused with missing source
configuration.
