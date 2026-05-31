// Package runtimebind registers the DocumentDB Elastic Clusters scanner with
// the awsruntime registry.
//
// The package has no exported surface. Importing it for its init side effect
// adds the DocumentDB Elastic scanner builder to the registry so
// DefaultScannerFactory can resolve service_kind "docdbelastic" without a
// central switch. Production callers pull every service binding through
// internal/collector/awscloud/awsruntime/bindings.
package runtimebind
