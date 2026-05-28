// Package runtimebind registers the AppSync scanner with the awsruntime
// registry.
//
// The package has no exported surface. Importing it for its init side effect
// adds the AppSync scanner builder to the registry so DefaultScannerFactory
// can resolve service_kind "appsync" without a central switch. The builder
// needs no redaction key because AppSync emits AWS-controlled metadata only and
// excludes secret-bearing fields by not mapping them. Production callers pull
// every service binding through
// internal/collector/awscloud/awsruntime/bindings.
package runtimebind
