# collector-tempo Agent Notes

This binary wires the live Tempo source package to workflow claims and
Postgres.

- Do not collect traces, spans, raw trace IDs, TraceQL bodies, tag values
  outside configured bounds, tenant values, credentials, or provider response
  bodies.
- Resolve credentials and tenant values only from environment variable names
  when the target uses `token_env` or `tenant_id_env`.
- Keep `ESHU_TEMPO_COLLECTOR_HEARTBEAT_INTERVAL` lower than
  `ESHU_TEMPO_COLLECTOR_CLAIM_LEASE_TTL`.
- Add focused config tests before changing environment variable names, target
  shape, or instance selection behavior.
