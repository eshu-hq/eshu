// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchhybrid

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/searchbench"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

type benchmarkBucketEmbedder struct {
	dims int
}

func (e benchmarkBucketEmbedder) Dimensions() int { return e.dims }

func (e benchmarkBucketEmbedder) Embed(_ context.Context, text string) ([]float64, error) {
	vector := make([]float64, e.dims)
	for _, token := range strings.Fields(text) {
		raw, ok := strings.CutPrefix(token, "bucket-")
		if !ok {
			continue
		}
		bucket, err := strconv.Atoi(raw)
		if err != nil {
			continue
		}
		vector[bucket%e.dims]++
	}
	return vector, nil
}

func BenchmarkBackendVectorRetrieval(b *testing.B) {
	const (
		docCount = 10000
		dims     = 64
	)
	docs := make([]searchdocs.Document, 0, docCount)
	for i := 0; i < docCount; i++ {
		bucket := i % dims
		id := "d-" + strconv.Itoa(i)
		bucketToken := "bucket-" + strconv.Itoa(bucket)
		docs = append(docs, doc(id, "repo-1", "component "+bucketToken, "vector retrieval "+bucketToken))
	}
	req := request("bucket-7", "repo-1", searchbench.ModeSemantic, 20)

	for _, tc := range []struct {
		name string
		mode VectorRetrievalMode
	}{
		{name: "exact", mode: VectorRetrievalExact},
		{name: "approximate", mode: VectorRetrievalApproximate},
	} {
		b.Run(tc.name, func(b *testing.B) {
			backend := Backend{Index: mustIndexB(b, docs, Options{
				Embedder:        benchmarkBucketEmbedder{dims: dims},
				VectorRetrieval: tc.mode,
			})}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := backend.Search(context.Background(), req); err != nil {
					b.Fatalf("Search error = %v", err)
				}
			}
		})
	}
}
