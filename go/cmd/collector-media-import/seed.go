// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	_ "embed"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// runSeedCommand parses the seed subcommand flags and materializes the seed
// payload: collector seed --dest <dir> --image-ref <repo@sha256:…>.
func runSeedCommand(args []string) error {
	flags := flag.NewFlagSet("seed", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dest := flags.String("dest", "", "destination directory for the seed payload")
	imageRef := flags.String("image-ref", "", "digest-pinned seed image reference (repo@sha256:<64hex>)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate caller binary: %w", err)
	}
	return runSeed(*dest, *imageRef, self)
}

// embeddedManifest carries the authored component manifest inside the caller
// binary so the seed dispatch materializes exactly the manifest the binary
// was built with, without reading the source tree at seed time.
//
//go:embed manifest.yaml
var embeddedManifest []byte

// runSeed validates the image reference and materializes the seed payload
// into dest: the caller binary under the satellite path name plus the
// embedded component manifest. It writes nothing on a validation failure,
// so a refused reference leaves no partial payload behind.
func runSeed(dest, imageRef, selfPath string) error {
	if err := validateSeedImageRef(imageRef); err != nil {
		return err
	}
	if strings.TrimSpace(dest) == "" {
		return fmt.Errorf("seed destination directory is required")
	}
	if strings.TrimSpace(selfPath) == "" {
		return fmt.Errorf("seed source binary path is required")
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return fmt.Errorf("create seed destination: %w", err)
	}
	if err := copySeedFile(selfPath, filepath.Join(dest, "collector"), 0o555); err != nil {
		return err
	}
	manifestPath := filepath.Join(dest, "manifest.yaml")
	if err := os.WriteFile(manifestPath, embeddedManifest, 0o444); err != nil {
		return fmt.Errorf("write seed manifest: %w", err)
	}
	return nil
}

// validateSeedImageRef fails closed on any reference that is not
// digest-pinned: the seed payload is only meaningful for exact bytes.
func validateSeedImageRef(imageRef string) error {
	repo, digest, found := strings.Cut(imageRef, "@")
	if !found || strings.TrimSpace(repo) == "" {
		return fmt.Errorf("--image-ref %q must be a digest-pinned reference (want repo@sha256:<64hex>)", imageRef)
	}
	lowered := strings.ToLower(strings.TrimSpace(digest))
	hex, ok := strings.CutPrefix(lowered, "sha256:")
	if !ok || len(hex) != 64 {
		return fmt.Errorf("--image-ref %q must be a digest-pinned reference (want repo@sha256:<64hex>)", imageRef)
	}
	for _, c := range hex {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return fmt.Errorf("--image-ref %q must be a digest-pinned reference (want repo@sha256:<64hex>)", imageRef)
		}
	}
	return nil
}

// copySeedFile copies one regular file to a destination path with the given
// permission bits.
func copySeedFile(src, dst string, perm os.FileMode) error {
	body, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read seed source: %w", err)
	}
	if err := os.WriteFile(dst, body, perm); err != nil {
		return fmt.Errorf("write seed payload: %w", err)
	}
	return nil
}
