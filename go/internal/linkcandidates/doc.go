// Package linkcandidates defines diagnostic link-prediction candidate evidence.
//
// The package validates candidate shape, truth labels, freshness, and bounded
// observation dimensions for future NornicDB link-prediction evaluation. It
// performs no graph writes, reducer admission, database I/O, API/MCP response
// shaping, or telemetry export; canonical relationship materialization remains
// a separate reducer-owned design.
package linkcandidates
