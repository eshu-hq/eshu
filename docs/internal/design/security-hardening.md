# Security Hardening — Epic H (#3732)

## Goal

Close the security defaults and rate-limit gaps so Eshu can be safely deployed
in production without operator-side overrides for every default.

## Decisions

### H1: Helm password defaults

**Decision:** Replace the hardcoded `change-me` default for
`neo4j.auth.password` with a Helm `randAlphaNum`-generated per-install random
password.

**Rationale:** A static default password is a known credential. The Helm
`randAlphaNum 20` helper generates a non-deterministic password on each
template invocation, ensuring no two installs share the same default. Operators
who capture the generated password and store it in a Kubernetes Secret (by
setting `neo4j.auth.secretName`) get a deterministic deployment.

**Trade-offs:**
- Non-deterministic `helm template` output means ArgoCD drift detection will
  flag every sync. Operators should set an explicit password or reference a
  K8s Secret before production deployment.
- The schema rejects weak passwords (change-me, password, admin, root, neo4j,
  eshu, etc.) via `not: {enum: [...]}` and requires mixed case + digits via
  `allOf: [{pattern: "[a-z]"}, {pattern: "[A-Z]"}, {pattern: "[0-9]"}]` with
  `minLength: 12`.

### H2: Bind address and NetworkPolicy defaults

**Decision:** Default the API server bind address to `127.0.0.1` and the
NetworkPolicy egress mode to `restricted`.

**Rationale:** Binding to `0.0.0.0` by default exposes the API on all network
interfaces, which is unnecessary for in-cluster communication. `restricted`
egress mode ensures workloads only reach explicitly configured destinations,
reducing blast radius from compromised containers.

**Trade-offs:**
- Operators who need external access must explicitly set
  `api.bindAddress: 0.0.0.0`.
- `restricted` egress requires operators to populate per-class destination
  selectors. The CI values file (`governance-two-team-k8s.values.yaml`)
  demonstrates the pattern.

### H3: OIDC login rate limiter

**Decision:** Add a layered token-bucket rate limiter (10 req/sec/IP, 60
req/min/user) using `golang.org/x/time/rate` on the OIDC login and callback
endpoints.

**Rationale:** Public-facing OIDC login endpoints without rate limiting are
vulnerable to credential-stuffing and pre-image attacks. A layered approach
(per-IP + per-user) prevents both distributed attacks from many IPs and
targeted attacks on a single user account.

**Trade-offs:**
- In-memory rate limiter state is not shared across API replicas. For
  multi-replica deployments, the effective per-IP limit is `N * rate` where N
  is the number of replicas. This is acceptable for now; a shared Redis-backed
  limiter can be added later.
- Rate limit is configurable via env vars
  (`ESHU_AUTH_OIDC_LOGIN_RATE_PER_SEC`, etc.) with sensible defaults.

### H5: SAML test coverage and constant-time comparison

**Decision:** Add comprehensive test coverage for SAML assertion validation,
replay fingerprinting, and claims normalization. Add `constantTimeHashEqual`
using `crypto/subtle.ConstantTimeCompare` for timing-safe hash comparison.

**Rationale:** The SAML package had tests only for metadata validation. The
assertion, claims, and hash functions form the core security boundary for SAML
response validation and need thorough coverage. Constant-time hash comparison
provides defense-in-depth against timing side channels when comparing replay
fingerprints and request ID hashes.

**Trade-offs:** None — pure test and helper addition with no behavioral change.

### H6: Browser session list pagination

**Decision:** Add `limit` (default 20, max 100) and `offset` query parameters to
`GET /api/v0/auth/sessions`. The store uses `LIMIT + 1` to detect truncation.
The response includes `prev` and `next` links when applicable.

**Rationale:** A 10K-session tenant would receive all results in one response
under the previous hardcoded `LIMIT 200`. Pagination bounds the response size
and enables progressive loading in the console UI.

**Trade-offs:** None — standard offset-based pagination that aligns with the
existing API conventions.
