// Package awsruntime adapts AWS cloud service scanners to workflow-claimed
// collector execution.
//
// The package owns claim parsing, target authorization, claim-scoped
// credential acquisition, scanner-side status updates, and collected-generation
// construction for AWS cloud work items. It also owns per-account concurrency,
// credential lease release, pagination checkpoint expiry, and production
// scanner registry introspection.
//
// Service scanners own AWS source observation and reducers own canonical graph
// truth. SupportedServiceKinds and SupportsServiceKind expose the production
// scanner registry to command-side startup validation.
package awsruntime
