# Incident projector namespace

This documentation-only package groups projector intent builders whose source
evidence belongs to the incident domain. It owns no runtime assembly, queue,
retry, storage, graph, or telemetry behavior.

The `routing` leaf selects PagerDuty incident-routing trigger facts and builds
one reducer intent. Root `internal/projector` owns invocation order and enqueue;
the resolution engine owns routing comparison and graph materialization.

This namespace is a readable ownership seam, not an independently extractable
service. A future repository split still needs explicit replacements for the
repository-internal fact, intent, and reducer-domain contracts.
