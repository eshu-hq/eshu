// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestNativeDockerfileRUNEmitsPHPModeAndInstallEvidence(t *testing.T) {
	t.Parallel()
	source := "FROM debian:12 AS builder\nRUN a2enmod php8.2\nFROM debian:12 AS runtime\nRUN a2enmod phpmyadmin\nRUN docker-php-ext-enable mysqli\nRUN a2enmod php8.3\n"
	_, envelopes := snapshotWithPolicy(t, parser.DefaultRegistry(), "Dockerfile", source)
	modeCount, builderInstallCount, runtimeInstallCount := 0, 0, 0
	for _, envelope := range envelopes {
		if envelope.FactKind != "content_entity" || envelope.Payload["relative_path"] != "Dockerfile" {
			continue
		}
		metadata, _ := envelope.Payload["entity_metadata"].(map[string]any)
		switch {
		case envelope.Payload["entity_type"] == "Variable" && envelope.Payload["entity_name"] == "php_server_mode:mod_php":
			modeCount++
			if envelope.Payload["start_line"] != 6 || metadata["source_kind"] != "a2enmod" || metadata["stage"] != "runtime" {
				t.Fatal("runtime PHP mode lost shared RUN line or stage provenance")
			}
		case envelope.Payload["entity_type"] == "DockerfileInstall" && envelope.Payload["entity_name"] == "php8.2":
			builderInstallCount++
			if envelope.Payload["start_line"] != 2 || metadata["manager"] != "a2enmod" || metadata["stage"] != "builder" {
				t.Fatal("builder install lost normalized command provenance")
			}
		case envelope.Payload["entity_type"] == "DockerfileInstall" && envelope.Payload["entity_name"] == "php8.3":
			runtimeInstallCount++
			if envelope.Payload["start_line"] != 6 || metadata["manager"] != "a2enmod" || metadata["stage"] != "runtime" {
				t.Fatal("runtime install lost normalized command provenance")
			}
		}
	}
	if modeCount != 1 || builderInstallCount != 1 || runtimeInstallCount != 1 {
		t.Fatalf("mode/builder-install/runtime-install counts = %d/%d/%d, want 1/1/1", modeCount, builderInstallCount, runtimeInstallCount)
	}
}
