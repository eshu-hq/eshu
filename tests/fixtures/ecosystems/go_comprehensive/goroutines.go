package comprehensive

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Worker processes jobs from a channel.
func Worker(id int, jobs <-chan int, results chan<- int, wg *sync.WaitGroup) {
	defer wg.Done()
	for job := range jobs {
		results <- job * 2
	}
}

// FanOut distributes work across multiple goroutines.
func FanOut(items []int, workers int) []int {
	jobs := make(chan int, len(items))
	results := make(chan int, len(items))

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go Worker(i, jobs, results, &wg)
	}

	for _, item := range items {
		jobs <- item
	}
	close(jobs)

	go func() {
		wg.Wait()
		close(results)
	}()

	var output []int
	for r := range results {
		output = append(output, r)
	}
	return output
}

// Ticker demonstrates select with channels.
func Ticker(ctx context.Context, interval time.Duration) <-chan string {
	ch := make(chan string)
	go func() {
		defer close(ch)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		count := 0
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				count++
				ch <- fmt.Sprintf("tick-%d", count)
			}
		}
	}()
	return ch
}
