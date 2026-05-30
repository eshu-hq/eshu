// Package runtimebind registers the Amazon Detective scanner with the awsruntime
// registry from a package init().
//
// Importing this package for its blank side effect is the only way a runtime
// brings the Detective scanner into the production registry. The package has no
// exported surface; it owns one awsruntime.Register call wiring
// awscloud.ServiceDetective to the Detective scanner builder.
package runtimebind
