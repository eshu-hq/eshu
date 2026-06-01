# collector-loki Agent Notes

This binary wires the live Loki source package to workflow claims and Postgres.

- Do not collect log lines, stream payloads, raw LogQL, label values outside the
  explicit allowlist, tenant values, credentials, or provider response bodies.
- Resolve credentials and tenant values only from environment variable names
  when the target uses `token_env` or `tenant_id_env`.
- Keep `ESHU_LOKI_COLLECTOR_HEARTBEAT_INTERVAL` lower than
  `ESHU_LOKI_COLLECTOR_CLAIM_LEASE_TTL`.
- Add focused config tests before changing environment variable names, target
  shape, or instance selection behavior.
