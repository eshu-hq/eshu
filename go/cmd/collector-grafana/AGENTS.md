# collector-grafana Agent Notes

This binary wires the live Grafana source package to workflow claims and
Postgres.

- Do not collect dashboard JSON, query bodies, contact destinations, tokens, or
  provider response bodies here.
- Resolve credentials only from environment variable names in collector config.
- Keep `ESHU_GRAFANA_COLLECTOR_HEARTBEAT_INTERVAL` lower than
  `ESHU_GRAFANA_COLLECTOR_CLAIM_LEASE_TTL`.
- Add focused config tests before changing environment variable names, target
  shape, or instance selection behavior.
