// Package awssdk adapts AWS SDK for Go v2 Lambda responses into scanner-owned
// Lambda records.
//
// The package owns Lambda API pagination, per-call telemetry, and response
// mapping for one claimed AWS boundary. It must not persist presigned package
// download URLs returned by GetFunction; callers receive only stable reported
// Lambda metadata for fact emission.
package awssdk
