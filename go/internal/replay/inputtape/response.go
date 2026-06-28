// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inputtape

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"sort"
)

// captureResponse reads and redacts resp into a RecordedResponse and returns a
// fresh ReadCloser over the body bytes so the caller can still read the live
// response after recording. The original resp.Body is fully read and closed.
func captureResponse(resp *http.Response, redaction redactionConfig) (RecordedResponse, io.ReadCloser, error) {
	var raw []byte
	if resp.Body != nil && resp.Body != http.NoBody {
		var err error
		raw, err = readAndClose(resp.Body)
		if err != nil {
			return RecordedResponse{}, nil, fmt.Errorf("read response body: %w", err)
		}
	}
	recorded := RecordedResponse{
		StatusCode: resp.StatusCode,
		Header:     redactHeader(resp.Header, redaction),
	}
	if resp.Body != nil && resp.Body != http.NoBody {
		recorded.Body = encodeBody(raw)
	}
	return recorded, newBodyReader(raw), nil
}

// buildResponse reconstructs an *http.Response from a recorded response, keyed to
// the replay request. The body is a fresh in-memory reader so the caller reads
// it exactly as it would a live response.
func buildResponse(req *http.Request, recorded RecordedResponse) (*http.Response, error) {
	body, err := decodeBody(recorded.Body)
	if err != nil {
		return nil, fmt.Errorf("inputtape: decode recorded body: %w", err)
	}
	header := make(http.Header, len(recorded.Header))
	for k, vs := range recorded.Header {
		// Preserve deterministic header value order on replay.
		cp := append([]string(nil), vs...)
		sort.Strings(cp)
		header[http.CanonicalHeaderKey(k)] = cp
	}
	resp := &http.Response{
		StatusCode:    recorded.StatusCode,
		Status:        fmt.Sprintf("%d %s", recorded.StatusCode, http.StatusText(recorded.StatusCode)),
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        header,
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       req,
	}
	return resp, nil
}
