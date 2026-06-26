# Security Hardening — Epic H (#3732)

## Goal

Close the security defaults and rate-limit gaps so Eshu can be safely deployed
in production without operator-side overrides for every default.

## Decisions

### H1: Helm password defaults

**Decision:** Replace the hardcoded `change-me` default for
`neo4j.auth.password` with an empty default. When no K8s Secret is referenced
(`neo4j.auth.secretName` is empty), the chart fails closed via Helm's
`required` function, forcing the operator to provide a strong password or
reference a K8s Secret.

**Rationale:** A static default password is a known credential. Helm's
`randAlphaNum` was evaluated but rejected: the helper runs on every `include`
of `eshu.renderNeo4jAuthEnv`, producing a different value for each workload
(api, mcp, ingester, reducer), none of which match the backend. Fail-closed
with a clear `required` error and schema validation is the standard Helm
pattern and avoids this divergence.

**Trade-offs:**
- Operators must explicitly set `neo4j.auth.password` or reference a K8s
  Secret before deployment. The schema validates the password is strong (not
  on a denylist of known-weak literals, minLength 12, mixed case + digit, or
  empty to allow secret-only flows).

### H2: Bind address and NetworkPolicy defaults

**Decision:** Keep the API server bind address at `0.0.0.0` (all pod-network
interfaces) and the NetworkPolicy egress mode at `broad` as the chart default.
Document `api.bindAddress: 127.0.0.1` for bare-host/docker-compose deployments
and `networkPolicy.egress.mode: restricted` as the hardened posture.

**Rationale:** In Kubernetes the kubelet connects to the pod IP for health
checks, not loopback. Binding to `127.0.0.1` causes liveness/readiness probes
to fail (pod never becomes Ready, Service endpoints empty, CrashLoopBackOff).
The isolation boundary in k8s is the pod netns; exposure is controlled by
Service type (`ClusterIP`) and NetworkPolicy ingress, not by binding to
loopback. Similarly, `restricted` egress with empty `datastores.to` and
`classes[].to` denies Postgres (5432), graph (7687), and all provider (443)
traffic, preventing readiness-checks from passing. Operators who need
restricted egress must populate the destination selectors.

**Trade-offs:**
- `broad` egress is the permissive default. Operators must explicitly opt into
  `restricted` and populate `datastores.to`, `graph`, and `classes[].to`
  selectors. The CI values file demonstrates the pattern.

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
