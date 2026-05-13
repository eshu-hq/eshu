# Harbor OCI Registry Adapter

This package owns Harbor-specific target normalization for the OCI registry
collector. Harbor repositories use the shared Distribution API client; this
package only validates the registry endpoint, rejects credential-bearing URLs,
normalizes repository paths, and builds the provider identity.

Robot account usernames and secrets are provided through runtime credential
environment variables. They must not appear in URLs, logs, facts, metric
labels, or docs.
