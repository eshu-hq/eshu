// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capabilitycatalog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// RemoteValidationInventoryFileName is the generated slug-to-row inventory
// stored beside the human-authored remote-validation evidence files.
const RemoteValidationInventoryFileName = "inventory.generated.json"

// RemoteValidationInventory is the deterministic list of production-supported
// remote_validation claims derived from the capability matrix.
type RemoteValidationInventory struct {
	SchemaVersion int                                 `json:"schema_version"`
	Artifacts     []RemoteValidationInventoryArtifact `json:"artifacts"`
}

// RemoteValidationInventoryArtifact binds one evidence slug to every
// production capability/profile row that cites it.
type RemoteValidationInventoryArtifact struct {
	Slug         string   `json:"slug"`
	ArtifactPath string   `json:"artifact_path"`
	Subjects     []string `json:"subjects"`
}

// BuildRemoteValidationInventory derives the checked-in evidence inventory
// from production-supported remote_validation rows in matrix.
func BuildRemoteValidationInventory(matrix Matrix) RemoteValidationInventory {
	subjectsByRef := map[string][]string{}
	for _, capability := range matrix.Capabilities {
		profile, ok := capability.Profiles[string(ProfileProduction)]
		if !ok || effectiveStatus(profile) != "supported" {
			continue
		}
		for _, verification := range profile.Verification {
			if verification.Kind != "remote_validation" {
				continue
			}
			subject := capability.Capability + "/" + string(ProfileProduction)
			subjectsByRef[verification.Ref] = append(subjectsByRef[verification.Ref], subject)
		}
	}

	refs := make([]string, 0, len(subjectsByRef))
	for ref := range subjectsByRef {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	artifacts := make([]RemoteValidationInventoryArtifact, 0, len(refs))
	for _, ref := range refs {
		artifacts = append(artifacts, RemoteValidationInventoryArtifact{
			Slug:         ref,
			ArtifactPath: RemoteValidationArtifactDir + "/" + ref + ".md",
			Subjects:     uniqueSortedStrings(subjectsByRef[ref]),
		})
	}
	return RemoteValidationInventory{SchemaVersion: 1, Artifacts: artifacts}
}

// MarshalRemoteValidationInventory renders inventory as stable, indented JSON
// with a trailing newline suitable for committing.
func MarshalRemoteValidationInventory(inventory RemoteValidationInventory) ([]byte, error) {
	var output bytes.Buffer
	fmt.Fprintf(&output, "{\n  \"schema_version\": %d,\n  \"artifacts\": [\n", inventory.SchemaVersion)
	for i, artifact := range inventory.Artifacts {
		raw, err := json.Marshal(artifact)
		if err != nil {
			return nil, fmt.Errorf("marshal remote-validation inventory artifact %q: %w", artifact.Slug, err)
		}
		comma := ","
		if i == len(inventory.Artifacts)-1 {
			comma = ""
		}
		fmt.Fprintf(&output, "    %s%s\n", raw, comma)
	}
	output.WriteString("  ]\n}\n")
	return output.Bytes(), nil
}

// CheckRemoteValidationInventory verifies that path is byte-for-byte current
// with the inventory derived from matrix.
func CheckRemoteValidationInventory(matrix Matrix, path string) error {
	want, err := MarshalRemoteValidationInventory(BuildRemoteValidationInventory(matrix))
	if err != nil {
		return err
	}
	got, err := os.ReadFile(path) // #nosec G304 -- caller supplies the fixed repository inventory path
	if err != nil {
		return fmt.Errorf("read remote-validation inventory %s: %w", path, err)
	}
	if !bytes.Equal(got, want) {
		return fmt.Errorf("remote-validation inventory %s is stale; regenerate it with the remote-validation update command", path)
	}
	return nil
}

// WriteRemoteValidationInventory regenerates path from matrix.
func WriteRemoteValidationInventory(matrix Matrix, path string) error {
	raw, err := MarshalRemoteValidationInventory(BuildRemoteValidationInventory(matrix))
	if err != nil {
		return err
	}
	// #nosec G301 -- this creates a public repository documentation directory,
	// whose generated inventory must remain readable by every checkout user.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create remote-validation inventory directory: %w", err)
	}
	// #nosec G306 -- this writes a committed public JSON artifact, not a secret.
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return fmt.Errorf("write remote-validation inventory %s: %w", path, err)
	}
	return nil
}
