// Package runtimebind registers the CodeBuild scanner with the awsruntime
// registry.
//
// The package has no exported surface. Importing it for its init side effect
// adds the CodeBuild scanner builder to the registry so DefaultScannerFactory
// can resolve service_kind "codebuild" without a central switch. The builder
// returns a typed error when the redaction key is zero because CodeBuild
// redacts PLAINTEXT environment-variable values. Production callers pull every
// service binding through internal/collector/awscloud/awsruntime/bindings.
package runtimebind
