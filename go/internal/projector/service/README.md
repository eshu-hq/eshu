# Service projector namespace

This documentation-only package groups projector intent builders whose source
evidence belongs to the service domain. It owns no runtime assembly, queue,
retry, storage, graph, or telemetry behavior.

The `catalog` leaf recognizes service-catalog facts for one scope generation
and builds the reducer intent that asks the reducer to correlate those catalog
declarations against repository and deployment truth. Root
`internal/projector` owns lookup construction, invocation order, and enqueue;
the reducer's `DomainServiceCatalogCorrelation` handler owns the correlation
decisions and every write.

This namespace is a readable ownership seam, not an independently extractable
service. A future repository split still needs explicit replacements for the
repository-internal fact, intent, and reducer-domain contracts.
