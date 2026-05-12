// Package ghcr adapts GitHub Container Registry repositories to Eshu's
// provider-neutral OCI registry contract.
//
// GHCR uses ghcr.io for both image references and Distribution calls, with a
// repository-scoped token endpoint for public and credential-backed pulls. This
// package keeps that auth shape outside the shared fact builders.
package ghcr
