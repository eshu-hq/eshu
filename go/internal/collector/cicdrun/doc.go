// Package cicdrun normalizes fixture-backed CI/CD provider evidence into
// durable facts.
//
// The package implements the first CI/CD run collector implementation slice:
// offline provider fixtures become reported-confidence facts for pipeline
// definitions, runs, jobs, steps, artifacts, trigger edges, environment
// observations, and warnings. It does not call hosted provider APIs, manage
// credentials, ingest logs, write graph state, or decide deployment truth.
// Reducers consume the emitted facts to correlate artifact, environment, and
// trigger evidence with stronger source, registry, cloud, or runtime truth.
package cicdrun
