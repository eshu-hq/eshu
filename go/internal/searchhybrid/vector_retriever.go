// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchhybrid

import (
	"fmt"
	"math"
)

const (
	approximateVectorTableCount    = 4
	approximateVectorSignatureBits = 12
)

type vectorRetriever interface {
	Score(queryVector []float64, inScope func(int) bool) map[int]float64
}

type exactVectorRetriever struct {
	index *Index
}

func (retriever exactVectorRetriever) Score(queryVector []float64, inScope func(int) bool) map[int]float64 {
	scores := make(map[int]float64)
	if !validVector(queryVector, retriever.index.vectorDims) {
		return scores
	}
	for i := range retriever.index.documents {
		if !inScope(i) {
			continue
		}
		vector := retriever.index.documents[i].vector
		if !validVector(vector, retriever.index.vectorDims) {
			continue
		}
		scores[i] = cosineSimilarity(queryVector, vector)
	}
	return scores
}

type approximateVectorRetriever struct {
	index  *Index
	exact  exactVectorRetriever
	tables []approximateVectorTable
}

type approximateVectorTable struct {
	buckets map[uint64][]int
}

func newVectorRetriever(index *Index, mode VectorRetrievalMode) (vectorRetriever, error) {
	exact := exactVectorRetriever{index: index}
	switch mode {
	case VectorRetrievalAuto:
		if index.count >= approximateVectorAutoMinDocuments {
			return newApproximateVectorRetriever(index, exact), nil
		}
		return exact, nil
	case VectorRetrievalExact:
		return exact, nil
	case VectorRetrievalApproximate:
		return newApproximateVectorRetriever(index, exact), nil
	default:
		return nil, fmt.Errorf("unsupported vector retrieval mode %q", mode)
	}
}

func newApproximateVectorRetriever(index *Index, exact exactVectorRetriever) approximateVectorRetriever {
	retriever := approximateVectorRetriever{
		index:  index,
		exact:  exact,
		tables: make([]approximateVectorTable, approximateVectorTableCount),
	}
	for table := range retriever.tables {
		retriever.tables[table] = approximateVectorTable{buckets: make(map[uint64][]int)}
	}
	for i := range index.documents {
		vector := index.documents[i].vector
		if !validVector(vector, index.vectorDims) {
			continue
		}
		for table := range retriever.tables {
			signature := angularVectorSignature(table, vector)
			retriever.tables[table].buckets[signature] = append(retriever.tables[table].buckets[signature], i)
		}
	}
	return retriever
}

func (retriever approximateVectorRetriever) Score(queryVector []float64, inScope func(int) bool) map[int]float64 {
	scores := make(map[int]float64)
	if !validVector(queryVector, retriever.index.vectorDims) {
		return scores
	}
	candidates := retriever.candidates(queryVector)
	if len(candidates) == 0 {
		return retriever.exact.Score(queryVector, inScope)
	}
	for idx := range candidates {
		if !inScope(idx) {
			continue
		}
		scores[idx] = cosineSimilarity(queryVector, retriever.index.documents[idx].vector)
	}
	if len(scores) == 0 {
		return retriever.exact.Score(queryVector, inScope)
	}
	return scores
}

func (retriever approximateVectorRetriever) candidates(queryVector []float64) map[int]struct{} {
	candidates := make(map[int]struct{})
	for tableIndex, table := range retriever.tables {
		signature := angularVectorSignature(tableIndex, queryVector)
		addApproximateBucketCandidates(candidates, table.buckets[signature])
		for bit := 0; bit < approximateVectorSignatureBits; bit++ {
			neighbor := signature ^ (uint64(1) << bit)
			addApproximateBucketCandidates(candidates, table.buckets[neighbor])
		}
	}
	return candidates
}

func addApproximateBucketCandidates(candidates map[int]struct{}, bucket []int) {
	for _, idx := range bucket {
		candidates[idx] = struct{}{}
	}
}

func validVector(vector []float64, dims int) bool {
	if dims <= 0 || len(vector) != dims {
		return false
	}
	nonZero := false
	for _, value := range vector {
		if !isFiniteFloat(value) {
			return false
		}
		if value != 0 {
			nonZero = true
		}
	}
	return nonZero
}

func isFiniteFloat(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

// angularVectorSignature projects vector through deterministic hyperplanes.
// Nearby angular vectors collide across at least one table with high
// probability, then final ranking uses exact cosine over the candidate set.
func angularVectorSignature(table int, vector []float64) uint64 {
	var signature uint64
	for bit := 0; bit < approximateVectorSignatureBits; bit++ {
		dot := 0.0
		for dim, value := range vector {
			dot += value * angularProjectionWeight(table, bit, dim)
		}
		if dot >= 0 {
			signature |= uint64(1) << bit
		}
	}
	return signature
}

func angularProjectionWeight(table int, bit int, dim int) float64 {
	seed := uint64(table+1)*0x9e3779b97f4a7c15 ^ // #nosec G115 -- bounded: table is a small loop index (< approximateVectorTableCount)
		uint64(bit+1)*0xbf58476d1ce4e5b9 ^ // #nosec G115 -- bounded: bit is a small loop index (< approximateVectorSignatureBits)
		uint64(dim+1)*0x94d049bb133111eb // #nosec G115 -- bounded: dim is a vector dimension index bounded by the vector length
	value := splitmix64(seed)
	const mantissaBits = 53
	unit := float64(value>>(64-mantissaBits)) / float64(uint64(1)<<mantissaBits)
	return unit*2 - 1
}

func splitmix64(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}
