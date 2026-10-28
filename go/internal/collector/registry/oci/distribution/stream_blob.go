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

// StreamBlob copies a verified digest blob to dst while enforcing a byte cap.
// Callers must treat dst as provisional and discard it on any error.
func (c *Client) StreamBlob(
	ctx context.Context,
	repository string,
	digest string,
	expectedSize int64,
	maxBytes int64,
	dst io.Writer,
) (int64, error) {
	if expectedSize < 0 || maxBytes <= 0 || expectedSize > maxBytes || dst == nil {
		return 0, fmt.Errorf("invalid OCI blob stream limit")
	}
	if len(digest) != len("sha256:")+sha256.Size*2 || !strings.HasPrefix(digest, "sha256:") {
		return 0, fmt.Errorf("invalid OCI blob digest")
	}
	for _, digit := range digest[len("sha256:"):] {
		if !strings.ContainsRune("0123456789abcdef", digit) {
			return 0, fmt.Errorf("invalid OCI blob digest")
		}
	}
	endpoint := "/v2/" + repositoryPath(repository) + "/blobs/" + url.PathEscape(digest)
	resp, err := c.do(ctx, "stream_blob", http.MethodGet, endpoint, map[string]string{"Accept": "*/*"})
	if err != nil {
		return 0, err
	}
	defer closeBody(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, statusError("stream_blob", resp)
	}
	hash := sha256.New()
	buffer := make([]byte, 64<<10)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		count, readErr := resp.Body.Read(buffer)
		if count > 0 {
			if int64(count) > maxBytes-total {
				return total, fmt.Errorf("OCI blob byte limit exceeded")
			}
			if _, err := hash.Write(buffer[:count]); err != nil {
				return total, fmt.Errorf("hash OCI blob: %w", err)
			}
			written, err := dst.Write(buffer[:count])
			if err != nil || written != count {
				return total, fmt.Errorf("write OCI blob destination")
			}
			total += int64(count)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return total, fmt.Errorf("read OCI blob body")
		}
	}
	if total != expectedSize || digest != fmt.Sprintf("sha256:%x", hash.Sum(nil)) {
		return total, fmt.Errorf("OCI blob descriptor mismatch")
	}
	return total, nil
}
