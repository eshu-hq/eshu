package searchhybrid

import (
	"fmt"
	"math"
	"strconv"
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
	index   *Index
	exact   exactVectorRetriever
	buckets map[string][]int
}

func newVectorRetriever(index *Index, mode VectorRetrievalMode) (vectorRetriever, error) {
	exact := exactVectorRetriever{index: index}
	switch mode {
	case VectorRetrievalAuto:
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
		index:   index,
		exact:   exact,
		buckets: make(map[string][]int),
	}
	for i := range index.documents {
		vector := index.documents[i].vector
		if !validVector(vector, index.vectorDims) {
			continue
		}
		signature := dominantVectorSignature(vector)
		retriever.buckets[signature] = append(retriever.buckets[signature], i)
	}
	return retriever
}

func (retriever approximateVectorRetriever) Score(queryVector []float64, inScope func(int) bool) map[int]float64 {
	scores := make(map[int]float64)
	if !validVector(queryVector, retriever.index.vectorDims) {
		return scores
	}
	for _, idx := range retriever.buckets[dominantVectorSignature(queryVector)] {
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

// dominantVectorSignature is a deterministic coarse angular bucket. It chooses
// the highest-magnitude dimension and sign, then scores only documents in the
// same bucket before falling back to exact retrieval when the scoped bucket is
// empty.
func dominantVectorSignature(vector []float64) string {
	bestIndex := 0
	bestMagnitude := 0.0
	for i, value := range vector {
		magnitude := math.Abs(value)
		if magnitude > bestMagnitude {
			bestIndex = i
			bestMagnitude = magnitude
		}
	}
	sign := "+"
	if vector[bestIndex] < 0 {
		sign = "-"
	}
	return strconv.Itoa(bestIndex) + sign
}
