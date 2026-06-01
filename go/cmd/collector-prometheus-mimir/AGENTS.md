# collector-prometheus-mimir Agent Notes

This binary wires the live Prometheus/Mimir source package to workflow claims
and Postgres.

- Do not collect metric samples, raw PromQL, target label values, tenant values,
  credentials, or provider response bodies here.
- Resolve credentials and tenant values only from environment variable names
  when the target uses `token_env` or `tenant_id_env`.
- Keep `ESHU_PROMETHEUS_MIMIR_COLLECTOR_HEARTBEAT_INTERVAL` lower than
  `ESHU_PROMETHEUS_MIMIR_COLLECTOR_CLAIM_LEASE_TTL`.
- Add focused config tests before changing environment variable names, target
  shape, or instance selection behavior.
