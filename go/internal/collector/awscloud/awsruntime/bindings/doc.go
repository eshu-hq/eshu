// Package bindings is a one-stop aggregator that imports every AWS service
// runtimebind package for its init side effect.
//
// The collector-aws-cloud command and external tests blank-import this
// package to ensure every production scanner is registered with awsruntime
// before DefaultScannerFactory resolves a claim. Adding a new scanner means
// adding one underscore-import line to bindings.go and no changes elsewhere.
package bindings
