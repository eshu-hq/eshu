// Package dockerhub adapts Docker Hub repositories to Eshu's provider-neutral
// OCI registry contract.
//
// Docker Hub uses docker.io as the image-reference host, registry-1.docker.io
// as the Distribution endpoint, and auth.docker.io for pull tokens. This
// package keeps those provider details outside the shared fact builders.
package dockerhub
