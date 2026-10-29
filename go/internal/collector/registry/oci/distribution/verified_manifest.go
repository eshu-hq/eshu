// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package distribution

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const maxVerifiedManifestBytes int64 = 1 << 20

// GetVerifiedManifest reads a bounded manifest addressed by its SHA-256 digest.
func (c *Client) GetVerifiedManifest(ctx context.Context, repository, digest string, maxBytes int64) (ManifestResponse, error) {
	if maxBytes <= 0 || maxBytes > maxVerifiedManifestBytes || !validSHA256Digest(digest) {
		return ManifestResponse{}, fmt.Errorf("invalid OCI manifest limit or digest")
	}
	endpoint := "/v2/" + repositoryPath(repository) + "/manifests/" + url.PathEscape(digest)
	resp, err := c.do(ctx, "get_verified_manifest", http.MethodGet, endpoint, map[string]string{
		"Accept": strings.Join([]string{
			"application/vnd.oci.image.manifest.v1+json",
			"application/vnd.oci.image.index.v1+json",
			"application/vnd.docker.distribution.manifest.v2+json",
			"application/vnd.docker.distribution.manifest.list.v2+json",
		}, ", "),
	})
	if err != nil {
		return ManifestResponse{}, err
	}
	defer closeBody(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ManifestResponse{}, statusError("get_verified_manifest", resp)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return ManifestResponse{}, fmt.Errorf("read OCI manifest body")
	}
	if int64(len(body)) > maxBytes {
		return ManifestResponse{}, fmt.Errorf("OCI manifest byte limit exceeded")
	}
	if digest != fmt.Sprintf("sha256:%x", sha256.Sum256(body)) {
		return ManifestResponse{}, fmt.Errorf("OCI manifest descriptor mismatch")
	}
	return ManifestResponse{
		Body:      body,
		Digest:    digest,
		MediaType: resp.Header.Get("Content-Type"),
		SizeBytes: int64(len(body)),
	}, nil
}

func validSHA256Digest(digest string) bool {
	if len(digest) != len("sha256:")+sha256.Size*2 || !strings.HasPrefix(digest, "sha256:") {
		return false
	}
	for _, digit := range digest[len("sha256:"):] {
		if !strings.ContainsRune("0123456789abcdef", digit) {
			return false
		}
	}
	return true
}
