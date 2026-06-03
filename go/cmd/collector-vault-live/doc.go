// Command collector-vault-live runs the read-only, metadata-only Vault
// collector for the secrets/IAM posture lane (#25, #1356). It snapshots each
// configured Vault cluster/namespace's metadata endpoints with a read-only
// token and commits redacted source facts through the shared collector commit
// boundary; it never reads a secret value.
package main
