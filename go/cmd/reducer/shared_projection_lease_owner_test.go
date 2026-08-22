// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const sharedProjectionLeaseOwnerHelperEnv = "ESHU_TEST_SHARED_PROJECTION_LEASE_OWNER_HELPER"

func TestBuildReducerServiceWiresSharedProjectionLeaseOwner(t *testing.T) {
	t.Parallel()

	db := &fakeReducerDB{}
	service, err := buildReducerService(
		context.Background(),
		db,
		stubGraphExecutor{},
		stubCypherExecutor{},
		postgres.NewSharedIntentStore(db),
		stubCypherReader{},
		stubCypherReader{},
		func(name string) string {
			if name == sharedProjectionLeaseOwnerEnv {
				return "shared-wiring-test"
			}
			return ""
		},
		nil,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("buildReducerService() error = %v, want nil", err)
	}
	if got := service.SharedProjectionRunner.Config.LeaseOwner; !strings.HasPrefix(got, "shared-wiring-test:") {
		t.Fatalf("buildReducerService() shared projection lease owner = %q, want configured prefix plus process identity", got)
	}
}

func TestLoadSharedProjectionLeaseOwnerUsesStableProcessIdentity(t *testing.T) {
	t.Parallel()

	getenv := func(name string) string {
		if name == sharedProjectionLeaseOwnerEnv {
			return "operator-prefix"
		}
		return ""
	}
	first := loadSharedProjectionLeaseOwner(getenv)
	second := loadSharedProjectionLeaseOwner(getenv)
	if first != second {
		t.Fatalf("shared projection lease owner changed within one process boot: %q != %q", first, second)
	}
	if first == "operator-prefix" || !strings.HasPrefix(first, "operator-prefix:") {
		t.Fatalf("shared projection lease owner = %q, want operator prefix plus per-process identity", first)
	}
	parts := strings.Split(first, ":")
	if len(parts) < 4 || strings.TrimSpace(parts[len(parts)-3]) == "" ||
		strings.TrimSpace(parts[len(parts)-2]) == "" || strings.TrimSpace(parts[len(parts)-1]) == "" {
		t.Fatalf("shared projection lease owner = %q, want non-empty hostname, pid, and boot nonce suffix", first)
	}
	if parts[len(parts)-2] != strconv.Itoa(os.Getpid()) {
		t.Fatalf("shared projection lease owner = %q, want current process pid before boot nonce", first)
	}
	if len(parts[len(parts)-1]) < 16 {
		t.Fatalf("shared projection lease owner = %q, want boot-unique nonce", first)
	}
}

func TestSharedProjectionLeaseOwnerDiffersAcrossProcessBoots(t *testing.T) {
	if os.Getenv(sharedProjectionLeaseOwnerHelperEnv) == "1" {
		fmt.Println(loadSharedProjectionLeaseOwner(func(string) string { return "" }))
		return
	}

	first := sharedProjectionLeaseOwnerFromHelperProcess(t)
	second := sharedProjectionLeaseOwnerFromHelperProcess(t)
	if first == second {
		t.Fatalf("shared projection lease owner reused across process boots: %q", first)
	}
	if !strings.HasPrefix(first, defaultSharedProjectionLeaseOwner+":") ||
		!strings.HasPrefix(second, defaultSharedProjectionLeaseOwner+":") {
		t.Fatalf("shared projection lease owner prefixes = %q / %q, want configured default prefix", first, second)
	}
}

func sharedProjectionLeaseOwnerFromHelperProcess(t *testing.T) string {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestSharedProjectionLeaseOwnerDiffersAcrossProcessBoots$")
	command.Env = append(os.Environ(), sharedProjectionLeaseOwnerHelperEnv+"=1")
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil {
		t.Fatalf("shared projection lease owner helper process: %v: %s", err, output.String())
	}
	for _, line := range strings.Split(output.String(), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, defaultSharedProjectionLeaseOwner+":") {
			return line
		}
	}
	t.Fatalf("shared projection lease owner helper output omitted owner: %q", output.String())
	return ""
}
