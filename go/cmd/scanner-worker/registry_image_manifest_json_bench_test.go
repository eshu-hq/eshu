// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"strings"
	"testing"
)

func BenchmarkValidateUniqueManifestJSONKeys(b *testing.B) {
	layer := `{"mediaType":"application/vnd.oci.image.layer.v1.tar+gzip","digest":"sha256:` + strings.Repeat("a", 64) + `","size":1234}`
	raw := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":` +
		`{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"sha256:` + strings.Repeat("b", 64) + `","size":123},"layers":[` +
		strings.TrimSuffix(strings.Repeat(layer+",", 6500), ",") + `]}`)
	if len(raw) > 1<<20 {
		b.Fatal("benchmark manifest exceeds transport cap")
	}
	b.SetBytes(int64(len(raw)))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := validateUniqueManifestJSONKeys(context.Background(), raw); err != nil {
			b.Fatal(err)
		}
	}
}
