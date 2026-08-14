// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package demo

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestDemoUp_BoundsTheComposePhase proves the up phase cannot hang forever.
//
// Observed on 2026-08-09 while measuring TTFA: `docker compose up -d --wait`
// sat at 0% CPU for 16 minutes with no containers created and had to be killed
// by hand. Only waitReady was bounded, so a wedged compose had no deadline at
// all -- and a measurement lane cannot produce a number on top of a phase that
// can run forever.
func TestDemoUp_BoundsTheComposePhase(t *testing.T) {
	t.Parallel()
	blocked := make(chan struct{})
	r := &Runtime{
		exec: func(ctx context.Context, _ []string, _ string, args ...string) ([]byte, error) {
			for _, a := range args {
				if a == "up" {
					close(blocked)
					<-ctx.Done() // hang exactly like the wedged compose did
					return nil, ctx.Err()
				}
			}
			return []byte(""), nil
		},
		probe:        func(context.Context, string, string) (IndexStatus, error) { return IndexStatus{}, nil },
		now:          time.Now,
		project:      "eshu-demo-test",
		composeFile:  "docker-compose.demo.yaml",
		upTimeout:    75 * time.Millisecond,
		pollInterval: time.Millisecond,
	}

	done := make(chan error, 1)
	go func() { _, err := r.Up(context.Background()); done <- err }()

	select {
	case <-blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("exec was never called with up")
	}

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("up() = nil, want a timeout error")
		}
		if !strings.Contains(err.Error(), "bring up demo stack") {
			t.Errorf("err = %v, want it to name the up phase", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("up() never returned; the compose phase is still unbounded")
	}
}

// TestDemoUp_SkipsTheBuildWhenImagesArePresent keeps instrumentation from
// slowing the thing it measures.
//
// Splitting build out of up so a cold run can be attributed cost 221,590 ms on
// a warm run measured on an idle host -- `docker compose build` revalidates
// every build context even when nothing changed, and `up -d --wait` already
// builds whatever is missing. So the explicit build runs only when an image is
// actually absent; otherwise the phase is recorded as zero and skipped.
func TestDemoUp_SkipsTheBuildWhenImagesArePresent(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		imagesPresent bool
		wantBuildRun  bool
	}{
		{"warm skips the build", true, false},
		{"cold runs the build", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var buildRan bool
			r := &Runtime{
				exec: func(_ context.Context, _ []string, _ string, args ...string) ([]byte, error) {
					joined := strings.Join(args, " ")
					switch {
					case strings.Contains(joined, "config --images"):
						return []byte("demo-a\ndemo-b\n"), nil
					case strings.Contains(joined, "image inspect"):
						if tc.imagesPresent {
							return []byte("[]"), nil
						}
						return nil, errors.New("no such image")
					case strings.Contains(joined, " build"):
						buildRan = true
						return []byte(""), nil
					}
					return []byte(""), nil
				},
				probe: func(context.Context, string, string) (IndexStatus, error) {
					return IndexStatus{Status: "healthy", RepositoryCount: 1}, nil
				},
				ask: func(context.Context, string, string) (Answer, error) {
					return Answer{Answer: "an answer", Truth: map[string]any{"level": "derived"}}, nil
				},
				now:          time.Now,
				project:      "eshu-demo-test",
				composeFile:  "docker-compose.demo.yaml",
				pollInterval: time.Millisecond,
			}
			res, err := r.Up(context.Background())
			if err != nil {
				t.Fatalf("up: %v", err)
			}
			if buildRan != tc.wantBuildRun {
				t.Errorf("explicit build ran = %v, want %v", buildRan, tc.wantBuildRun)
			}
			if _, ok := res.PhaseMillis["build"]; !ok {
				t.Error("build phase missing; the scorer requires every phase to be recorded")
			}
		})
	}
}
