// Package runtimebind registers the VPC Lattice scanner with the awsruntime
// registry.
//
// The package has no exported surface. Importing it for its init side effect
// adds the VPC Lattice scanner builder to the registry so DefaultScannerFactory
// can resolve service_kind "vpclattice" without a central switch. Production
// callers pull every service binding through
// internal/collector/awscloud/awsruntime/bindings.
package runtimebind
