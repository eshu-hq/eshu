// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package replay_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/replay"
)

// sharedWriterVector is the cross-tree canonical-writer vector: one scope, two
// facts out of order, number literals with a trailing zero, an exponent and a
// value wider than a float64, HTML characters, and payload keys that look like
// the volatile envelope keys. The vulnerability collector satellite
// (eshu-hq/eshu-collector-vulnerability-intelligence) re-implements the
// canonical writer so it can record the cassette Eshu commits, and pins this
// same text and the same digest against its own writer, so a change to either
// side's canonical rules fails a test in that tree. Change the text and the
// digest in both trees together (eshu-hq/eshu#10734).
const sharedWriterVector = `{
  "scopes": [
    {
      "observed_at": "2026-11-15T12:34:56.789Z",
      "generation_id": "vulnerability_intelligence:run-specific-9",
      "scope_id": "vuln-intel://vector/scope",
      "source_system": "vulnerability_intelligence",
      "scope_kind": "vulnerability_intelligence",
      "collector_kind": "vulnerability_intelligence",
      "partition_key": "vulnerability_intelligence:vector-instance:nvd",
      "metadata": {"instance_id": "vector-instance", "component_id": "dev.eshu.collector.vulnerability_intelligence"},
      "trigger_kind": "snapshot",
      "facts": [
        {
          "stable_fact_key": "key-b",
          "fact_kind": "vulnerability.reference",
          "schema_version": "1.0.0",
          "source_confidence": "reported",
          "source_uri": "https://vector.example.test/b?x=1&y=<2>",
          "source_record_id": "record-b",
          "payload": {"url": "https://vector.example.test/b?x=1&y=<2>", "note": "fish & chips <b>bold</b>", "observed_at": "payload-keeps-this", "generation_id": "payload-keeps-this-too", "ratio": 1.50, "count": 10}
        },
        {
          "stable_fact_key": "key-a",
          "fact_kind": "vulnerability.cve",
          "schema_version": "1.0.0",
          "source_confidence": "reported",
          "source_uri": "https://vector.example.test/a",
          "source_record_id": "record-a",
          "payload": {"cve_id": "CVE-2026-90000", "cvss_score": 9.80, "big": 12345678901234567890, "exp": 1e3, "nested": {"z": 1, "a": [3, 2, 1]}}
        }
      ]
    }
  ],
  "schema_version": "1",
  "collector": "vector"
}
`

// sharedWriterVectorDigest is the sha256 of the canonical bytes
// replay.Canonicalize writes for sharedWriterVector under
// DefaultCanonicalOptions. The collector pins the same value.
const sharedWriterVectorDigest = "sha256:c57e080d2df346839eb242941c173e79e393bb5ee81c7936ce5513bed3815e2d"

func TestCanonicalizeMatchesSharedWriterVector(t *testing.T) {
	t.Parallel()

	out, err := replay.Canonicalize([]byte(sharedWriterVector), replay.DefaultCanonicalOptions())
	if err != nil {
		t.Fatalf("Canonicalize(shared vector) error = %v, want nil", err)
	}
	sum := sha256.Sum256(out)
	if got := "sha256:" + hex.EncodeToString(sum[:]); got != sharedWriterVectorDigest {
		t.Fatalf("canonical shared vector digest = %s, want %s: the satellite's writer pins the same digest, so "+
			"change the vector and the digest in both trees together\n%s", got, sharedWriterVectorDigest, out)
	}
}
