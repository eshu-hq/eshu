package eshulocal

import (
	"errors"
	"testing"
)

func TestPrepareWorkspace(t *testing.T) {
	t.Run("runs startup steps in lock version reclaim order", func(t *testing.T) {
		layout := testLayout(t)
		var calls []string

		lock, err := PrepareWorkspace(layout, "v1", StartupDeps{
			AcquireLock: func(path string) (*OwnerLock, error) {
				calls = append(calls, "lock")
				return &OwnerLock{}, nil
			},
			ReleaseLock: func(lock *OwnerLock) error {
				calls = append(calls, "release")
				return nil
			},
			EnsureVersion: func(layout Layout, currentVersion string) error {
				calls = append(calls, "version")
				return nil
			},
			ValidateOrReclaim: func(layout Layout, currentVersion string, deps ReclaimDeps) error {
				calls = append(calls, "reclaim")
				return nil
			},
		})
		if err != nil {
			t.Fatalf("PrepareWorkspace() error = %v, want nil", err)
		}
		if lock == nil {
			t.Fatal("PrepareWorkspace() lock = nil, want non-nil")
		}
		want := []string{"lock", "version", "reclaim"}
		if len(calls) != len(want) {
			t.Fatalf("call count = %d, want %d", len(calls), len(want))
		}
		for i := range want {
			if calls[i] != want[i] {
				t.Fatalf("calls[%d] = %q, want %q", i, calls[i], want[i])
			}
		}
	})

	t.Run("version failure releases lock and skips reclaim", func(t *testing.T) {
		layout := testLayout(t)
		var calls []string
		wantErr := errors.New("bad version")

		lock, err := PrepareWorkspace(layout, "v1", StartupDeps{
			AcquireLock: func(path string) (*OwnerLock, error) {
				calls = append(calls, "lock")
				return &OwnerLock{}, nil
			},
			ReleaseLock: func(lock *OwnerLock) error {
				calls = append(calls, "release")
				return nil
			},
			EnsureVersion: func(layout Layout, currentVersion string) error {
				calls = append(calls, "version")
				return wantErr
			},
			ValidateOrReclaim: func(layout Layout, currentVersion string, deps ReclaimDeps) error {
				calls = append(calls, "reclaim")
				return nil
			},
		})
		if lock != nil {
			t.Fatalf("PrepareWorkspace() lock = %#v, want nil", lock)
		}
		if !errors.Is(err, wantErr) {
			t.Fatalf("PrepareWorkspace() error = %v, want %v", err, wantErr)
		}
		want := []string{"lock", "version", "release"}
		if len(calls) != len(want) {
			t.Fatalf("call count = %d, want %d", len(calls), len(want))
		}
		for i := range want {
			if calls[i] != want[i] {
				t.Fatalf("calls[%d] = %q, want %q", i, calls[i], want[i])
			}
		}
	})

	t.Run("reclaim failure releases lock", func(t *testing.T) {
		layout := testLayout(t)
		var calls []string
		wantErr := errors.New("stale owner")

		lock, err := PrepareWorkspace(layout, "v1", StartupDeps{
			AcquireLock: func(path string) (*OwnerLock, error) {
				calls = append(calls, "lock")
				return &OwnerLock{}, nil
			},
			ReleaseLock: func(lock *OwnerLock) error {
				calls = append(calls, "release")
				return nil
			},
			EnsureVersion: func(layout Layout, currentVersion string) error {
				calls = append(calls, "version")
				return nil
			},
			ValidateOrReclaim: func(layout Layout, currentVersion string, deps ReclaimDeps) error {
				calls = append(calls, "reclaim")
				return wantErr
			},
		})
		if lock != nil {
			t.Fatalf("PrepareWorkspace() lock = %#v, want nil", lock)
		}
		if !errors.Is(err, wantErr) {
			t.Fatalf("PrepareWorkspace() error = %v, want %v", err, wantErr)
		}
		want := []string{"lock", "version", "reclaim", "release"}
		if len(calls) != len(want) {
			t.Fatalf("call count = %d, want %d", len(calls), len(want))
		}
		for i := range want {
			if calls[i] != want[i] {
				t.Fatalf("calls[%d] = %q, want %q", i, calls[i], want[i])
			}
		}
	})
}
