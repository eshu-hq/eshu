package comprehensive

import (
	"fmt"
	"strings"
)

// Greet returns a greeting for the given name.
func Greet(name string) string {
	return fmt.Sprintf("Hello, %s!", name)
}

// Add returns the sum of two integers.
func Add(a, b int) int {
	return a + b
}

// Divide performs division with error handling.
func Divide(a, b float64) (float64, error) {
	if b == 0 {
		return 0, fmt.Errorf("division by zero")
	}
	return a / b, nil
}

// Swap returns values in reverse order using named returns.
func Swap(a, b string) (first, second string) {
	first = b
	second = a
	return
}

// JoinAll joins variadic string arguments.
func JoinAll(sep string, parts ...string) string {
	return strings.Join(parts, sep)
}

// ProcessItems processes items with a callback.
func ProcessItems(items []string, fn func(string) string) []string {
	result := make([]string, len(items))
	for i, item := range items {
		result[i] = fn(item)
	}
	return result
}
