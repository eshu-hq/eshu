// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"path/filepath"
	"sync"
	"time"
)

type concurrentSCIPIndexer struct {
	mu          sync.Mutex
	available   map[string]bool
	active      int
	max         int
	delay       time.Duration
	barrier     chan struct{}
	waitFor     int
	barrierOnce sync.Once
}

func (i *concurrentSCIPIndexer) IsAvailable(language string) bool {
	return i.available[language]
}

func (i *concurrentSCIPIndexer) Run(ctx context.Context, projectPath string, language string, outputDir string) (string, error) {
	i.mu.Lock()
	i.active++
	if i.active > i.max {
		i.max = i.active
	}
	if i.barrier != nil && i.active >= i.waitFor {
		i.barrierOnce.Do(func() { close(i.barrier) })
	}
	barrier := i.barrier
	i.mu.Unlock()
	defer func() {
		i.mu.Lock()
		i.active--
		i.mu.Unlock()
	}()

	if barrier != nil {
		select {
		case <-barrier:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	} else {
		select {
		case <-time.After(i.delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}

	return filepath.Join(outputDir, language+".scip"), nil
}

func (i *concurrentSCIPIndexer) maxActive() int {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.max
}
