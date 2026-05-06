package comprehensive

// Counter returns a closure that increments a counter.
func Counter() func() int {
	count := 0
	return func() int {
		count++
		return count
	}
}

// Adder returns a closure that adds a fixed value.
func Adder(base int) func(int) int {
	return func(n int) int {
		return base + n
	}
}

// Compose composes two functions.
func Compose(f, g func(int) int) func(int) int {
	return func(x int) int {
		return f(g(x))
	}
}

// Apply applies a function to a value.
func Apply(value int, fns ...func(int) int) int {
	result := value
	for _, fn := range fns {
		result = fn(result)
	}
	return result
}
