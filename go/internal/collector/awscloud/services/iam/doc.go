// Package iam scans AWS IAM source truth into AWS cloud fact observations.
//
// It emits IAM roles, users, managed policies, instance profiles, trust
// principals, and IAM relationships, plus derived aws_iam_permission facts: the
// normalized, metadata-only projection of inline, attached managed, and role
// trust policy statements (effect, action set, resource pattern, condition-key
// summary). The scanner never holds or emits the raw policy JSON body or
// condition values; the SDK adapter normalizes documents at the wiring boundary.
//
// The package defines scanner-owned client interfaces so unit tests can inject
// fakes without mocking the full AWS SDK for Go v2 surface. SDK adapters belong
// at runtime wiring boundaries.
package iam
