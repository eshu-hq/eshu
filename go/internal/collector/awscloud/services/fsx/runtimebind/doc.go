// Package runtimebind registers the FSx scanner with the awsruntime registry.
//
// The package has no exported surface. Importing it for its init side effect
// adds the FSx scanner builder to the registry so DefaultScannerFactory can
// resolve service_kind "fsx" without a central switch. Production callers pull
// every service binding through
// internal/collector/awscloud/awsruntime/bindings.
package runtimebind
