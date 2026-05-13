// Package iam scans AWS IAM source truth into AWS cloud fact observations.
//
// The package defines scanner-owned client interfaces so unit tests can inject
// fakes without mocking the full AWS SDK for Go v2 surface. SDK adapters belong
// at runtime wiring boundaries.
package iam
