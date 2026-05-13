# Google Artifact Registry OCI Adapter

This package owns Google Artifact Registry target normalization for Docker
repositories. GAR repositories use location-scoped docker.pkg.dev registry
hosts and the shared Distribution API client.

Credential helpers, short-lived access tokens, and service account credentials
are resolved outside this package and passed to the collector runtime through
environment-backed username/password or bearer-token fields.
