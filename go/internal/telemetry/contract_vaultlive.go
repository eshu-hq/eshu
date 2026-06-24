// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

// Metric dimension keys and bounded label values for the live Vault collector
// lane live in this sibling file so the frozen contract.go does not grow with
// each new lane, mirroring contract_scanner_worker.go.
const (
	// MetricDimensionFieldClass labels redaction metrics with the bounded class
	// of field that was redacted, never the redacted value itself. The Vault
	// lane uses it on eshu_dp_secrets_iam_source_redactions_total with the
	// closed FieldClass* values below so an operator can answer "which
	// credential-bearing field shapes are being stripped from Vault source
	// provenance?" without ever seeing the secret material.
	MetricDimensionFieldClass = "field_class"
)

// Bounded field_class label values for eshu_dp_secrets_iam_source_redactions_total.
// They name the shape of the stripped field, never its content, so the metric
// stays low-cardinality and leak-free.
const (
	// FieldClassURIUserinfo marks a source URI whose basic-auth userinfo
	// component (user[:password]) was stripped before the URI reached a fact's
	// SourceRef.
	FieldClassURIUserinfo = "uri_userinfo"
	// FieldClassURIQuery marks a source URI whose query string (which may carry
	// a token parameter) was stripped before the URI reached a fact's SourceRef.
	FieldClassURIQuery = "uri_query"
	// FieldClassURIFragment marks a source URI whose fragment component was
	// stripped before the URI reached a fact's SourceRef.
	FieldClassURIFragment = "uri_fragment"
)

func init() {
	metricDimensionKeys = append(metricDimensionKeys, MetricDimensionFieldClass)
}
