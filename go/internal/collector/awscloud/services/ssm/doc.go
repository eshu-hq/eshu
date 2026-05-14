// Package ssm converts AWS Systems Manager Parameter Store metadata into AWS
// collector fact envelopes.
//
// Scanner accepts only the SSM service boundary and emits reported parameter
// metadata plus direct KMS relationship evidence for SecureString parameters.
// It deliberately excludes parameter values, history values, raw descriptions,
// raw allowed patterns, raw policy JSON, and mutations so downstream reducers
// can reason about dependencies without the collector handling secret material.
package ssm
