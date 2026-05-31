// Package runtimebind registers the AppConfig scanner with the awsruntime
// registry.
//
// The package has no exported surface. Importing it for its init side effect
// adds the AppConfig scanner builder to the registry so DefaultScannerFactory
// can resolve service_kind "appconfig" without a central switch. Production
// callers pull every service binding through
// internal/collector/awscloud/awsruntime/bindings.
package runtimebind
