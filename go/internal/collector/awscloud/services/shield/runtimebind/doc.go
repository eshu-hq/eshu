// Package runtimebind registers the Shield Advanced scanner with the awsruntime
// registry.
//
// The package has no exported surface. Importing it for its init side effect
// adds the Shield Advanced scanner builder to the registry so
// DefaultScannerFactory can resolve service_kind "shield" without a central
// switch. Production callers pull every service binding through
// internal/collector/awscloud/awsruntime/bindings.
package runtimebind
