package comprehensive

import "fmt"

// Ordered is a type constraint for ordered types.
type Ordered interface {
	~int | ~float64 | ~string
}

// Min returns the minimum of two ordered values.
func Min[T Ordered](a, b T) T {
	if a < b {
		return a
	}
	return b
}

// Map applies a function to each element.
func Map[T any, U any](items []T, fn func(T) U) []U {
	result := make([]U, len(items))
	for i, item := range items {
		result[i] = fn(item)
	}
	return result
}

// Filter returns elements matching the predicate.
func Filter[T any](items []T, predicate func(T) bool) []T {
	var result []T
	for _, item := range items {
		if predicate(item) {
			result = append(result, item)
		}
	}
	return result
}

// Stack is a generic stack data structure.
type Stack[T any] struct {
	items []T
}

func (s *Stack[T]) Push(item T) {
	s.items = append(s.items, item)
}

func (s *Stack[T]) Pop() (T, bool) {
	if len(s.items) == 0 {
		var zero T
		return zero, false
	}
	item := s.items[len(s.items)-1]
	s.items = s.items[:len(s.items)-1]
	return item, true
}

// Pair is a generic pair type.
type Pair[A, B any] struct {
	First  A
	Second B
}

func NewPair[A, B any](a A, b B) Pair[A, B] {
	return Pair[A, B]{First: a, Second: b}
}

func (p Pair[A, B]) String() string {
	return fmt.Sprintf("(%v, %v)", p.First, p.Second)
}
