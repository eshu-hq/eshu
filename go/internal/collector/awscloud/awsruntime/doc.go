// Package awsruntime adapts AWS cloud service scanners to workflow-claimed
// collector execution.
//
// The package owns claim parsing, target authorization, claim-scoped
// credential acquisition, scanner-side status updates, and collected-generation
// construction for AWS cloud work items. It also owns per-account concurrency,
// credential lease release, pagination checkpoint expiry, and a package-level
// scanner registry that production runtimes populate at process start
// through service runtimebind packages.
//
// Service scanners own AWS source observation and reducers own canonical graph
// truth. SupportedServiceKinds and SupportsServiceKind report the registered
// production scanner set, including metadata-only families such as GuardDuty,
// to command-side startup validation. The collector-aws-cloud command
// blank-imports awsruntime/bindings to install every scanner before
// DefaultScannerFactory dispatches the first claim.
package awsruntime
