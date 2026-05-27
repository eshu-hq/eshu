// Package runtimebind binds the CloudWatch service scanner into the
// awsruntime registry. Importing this package for its init side effect adds
// the production scanner to the registry without modifying any shared file.
//
// The binder validates the runtime-provided RedactionKey before constructing
// the scanner because customer-tag-named alarm metric dimensions must be
// redacted through the shared library before persistence.
package runtimebind
