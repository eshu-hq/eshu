// Package serviceintelhttp is the HTTP adapter that serves the service
// intelligence report route, GET /api/v0/services/{service_name}/intelligence-report.
//
// It is the single composition point that joins two lower layers without an
// import cycle: it calls the query package's BuildServiceStoryEnvelope seam to
// build the service-story dossier and truth, then composes the report with the
// pure serviceintel composer. The query package imports neither this package nor
// serviceintel, so the dependency flows one way (serviceintelhttp -> {query,
// serviceintel}; serviceintel -> query).
//
// The handler runs no LLM path and re-derives no truth: the report's truth is the
// service-story truth, and resolution failures (capability gate, scoped-token
// access, ambiguous or missing service) return the same error envelope as the
// service-story route. The supply_chain and incidents_support sections stay
// unsupported with their fallback next calls until their evidence lanes gain a
// reusable seam.
package serviceintelhttp
