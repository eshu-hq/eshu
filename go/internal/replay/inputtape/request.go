// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inputtape

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/eshu-hq/eshu/go/internal/replay"
)

// requestKey reduces a request to its deterministic match key. The key is a
// SHA-256 over method, path, the sorted query, and (when present) the
// canonicalized request body. It does not depend on header order, query order,
// or — for JSON bodies — key order, so a collector that builds an equivalent
// request resolves to the same recorded interaction across runs.
//
// Redaction is applied to the query before hashing so a secret query parameter
// never participates in the key (the key must be derivable from a credential-
// free replay request, which carries the redaction sentinel in its place). The
// returned body capture is the redacted, canonical body the recorder persists.
//
// requestKey reads and replaces req.Body when a body is present so the caller's
// downstream read still sees the full body; the caller MUST use the returned
// request for any further use of the body.
func requestKey(req *http.Request, redaction redactionConfig) (string, RecordedRequest, *http.Request, error) {
	body, restored, err := captureBody(req, true)
	if err != nil {
		return "", RecordedRequest{}, req, err
	}

	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}
	path := req.URL.EscapedPath()
	query := redactQuery(req.URL.Query(), redaction)

	// The key uses a normalized query where volatile params (per-run timestamps,
	// nonces) collapse to a fixed sentinel, so a replay request with a different
	// timestamp still matches the recording. Secrets are already collapsed in
	// `query`; build the key off that and then normalize volatiles.
	keyQuery := keyQueryView(query, redaction)
	keyMaterial := keyDocument(method, path, keyQuery, body)
	sum := sha256.Sum256([]byte(keyMaterial))

	recorded := RecordedRequest{
		Method: method,
		Path:   path,
		Query:  query,
		Header: redactHeader(req.Header, redaction),
		Body:   body,
	}
	return hex.EncodeToString(sum[:]), recorded, restored, nil
}

// keyDocument builds the canonical string the request key hashes. Query keys and
// their values are sorted; the body's encoding and data are appended so two
// requests that differ only in body resolve to different keys, and a JSON body
// (already canonicalized) yields a key independent of original key order.
func keyDocument(method, path string, query map[string][]string, body RecordedBody) string {
	var b strings.Builder
	b.WriteString(method)
	b.WriteByte('\n')
	b.WriteString(path)
	b.WriteByte('\n')

	keys := make([]string, 0, len(query))
	for k := range query {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte('=')
		// Encode each value length-prefixed (e.g. "1:a3:b,c") rather than joining
		// with a separator: a separator like "," is ambiguous when values
		// themselves contain it, so ?match=a,b&match=c and ?match=a&match=b,c
		// would otherwise collapse to the same key and one interaction would
		// overwrite the other. Repeated params with comma-bearing selectors
		// (PromQL/LogQL match[]) hit this exactly. Values are already sorted by
		// redactQuery, so equal value sets in any source order still hash equally.
		for _, v := range query[k] {
			b.WriteString(strconv.Itoa(len(v)))
			b.WriteByte(':')
			b.WriteString(v)
		}
		b.WriteByte('\n')
	}

	b.WriteByte('\n')
	if body.Present {
		b.WriteString(string(body.Encoding))
		b.WriteByte('\n')
		b.WriteString(body.Data)
	}
	return b.String()
}

// redactQuery returns a copy of the query with each value list sorted and any
// secret parameter's values replaced by the redaction sentinel.
func redactQuery(query url.Values, redaction redactionConfig) map[string][]string {
	if len(query) == 0 {
		return nil
	}
	out := make(map[string][]string, len(query))
	for k, vs := range query {
		if redaction.isSecretQueryParam(k) {
			out[k] = []string{replay.RedactedSentinel}
			continue
		}
		cp := append([]string(nil), vs...)
		sort.Strings(cp)
		out[k] = cp
	}
	return out
}

// volatileKeySentinel replaces a volatile query parameter's value in the request
// key (not in the stored request, which keeps the real value for review). Two
// runs that differ only in a volatile parameter therefore produce the same key.
const volatileKeySentinel = "<volatile>"

// keyQueryView returns the query used to compute the request key. It starts from
// the already-redacted query (secrets collapsed) and additionally collapses any
// volatile parameter to volatileKeySentinel, so a per-run timestamp or nonce
// does not break replay matching. The stored recorded request keeps the real
// (non-secret) value; only the key view normalizes it.
func keyQueryView(redactedQuery map[string][]string, redaction redactionConfig) map[string][]string {
	if len(redactedQuery) == 0 {
		return nil
	}
	out := make(map[string][]string, len(redactedQuery))
	for k, vs := range redactedQuery {
		if redaction.isVolatileQueryParam(k) {
			out[k] = []string{volatileKeySentinel}
			continue
		}
		out[k] = vs
	}
	return out
}

// redactHeader returns a copy of the headers with each value list sorted and any
// secret-bearing header's values replaced by the redaction sentinel. The
// canonical (textproto) header key casing from req.Header is preserved.
func redactHeader(header http.Header, redaction redactionConfig) map[string][]string {
	if len(header) == 0 {
		return nil
	}
	out := make(map[string][]string, len(header))
	for k, vs := range header {
		if redaction.isSecretHeader(k) {
			out[k] = []string{replay.RedactedSentinel}
			continue
		}
		cp := append([]string(nil), vs...)
		sort.Strings(cp)
		out[k] = cp
	}
	return out
}

// captureBody reads body from r (request or response) and returns its recorded
// form plus, when isRequest is true, a shallow copy of the request whose Body is
// restored to a fresh reader over the same bytes. JSON bodies are canonicalized
// so the tape stays reviewable and the request key is key-order independent;
// UTF-8 non-JSON bodies are stored verbatim; everything else is base64.
func captureBody(r *http.Request, isRequest bool) (RecordedBody, *http.Request, error) {
	if r.Body == nil || r.Body == http.NoBody {
		return RecordedBody{}, r, nil
	}
	raw, err := readAndClose(r.Body)
	if err != nil {
		return RecordedBody{}, r, fmt.Errorf("read request body: %w", err)
	}
	recorded := encodeBody(raw)
	if !isRequest {
		return recorded, r, nil
	}
	restored := r.Clone(r.Context())
	restored.Body = newBodyReader(raw)
	restored.ContentLength = int64(len(raw))
	return recorded, restored, nil
}

// encodeBody classifies and encodes raw body bytes for the tape. An empty slice
// is recorded as a present-but-empty body so a deliberate zero-length body is
// distinguished from no body at all.
func encodeBody(raw []byte) RecordedBody {
	if len(raw) == 0 {
		return RecordedBody{Present: true, Encoding: BodyEncodingText, Data: ""}
	}
	if canonical, err := replay.Canonicalize(raw, bodyCanonicalOptions()); err == nil {
		return RecordedBody{Present: true, Encoding: BodyEncodingJSON, Data: string(canonical)}
	}
	if utf8.Valid(raw) {
		return RecordedBody{Present: true, Encoding: BodyEncodingText, Data: string(raw)}
	}
	return RecordedBody{Present: true, Encoding: BodyEncodingBase64, Data: base64.StdEncoding.EncodeToString(raw)}
}

// decodeBody reconstructs the raw bytes a RecordedBody stored, inverting
// encodeBody. A JSON body returns its canonical bytes (collectors parse JSON, so
// canonical form is equivalent for replay) and a base64 body is decoded.
func decodeBody(body RecordedBody) ([]byte, error) {
	if !body.Present {
		return nil, nil
	}
	switch body.Encoding {
	case BodyEncodingJSON, BodyEncodingText:
		return []byte(body.Data), nil
	case BodyEncodingBase64:
		decoded, err := base64.StdEncoding.DecodeString(body.Data)
		if err != nil {
			return nil, fmt.Errorf("decode base64 body: %w", err)
		}
		return decoded, nil
	default:
		return nil, fmt.Errorf("unknown body encoding %q", body.Encoding)
	}
}

// bodyCanonicalOptions returns the canonical options for HTTP bodies: object
// keys are sorted (so the key is key-order independent) but no field-specific
// volatile/derived normalization applies, because an arbitrary provider body has
// no known schema. Secret redaction inside bodies is intentionally not attempted
// here; secrets travel in headers/query at this boundary, and over-redacting
// body fields by name would corrupt provider payloads the collector parses.
func bodyCanonicalOptions() replay.CanonicalOptions {
	return replay.CanonicalOptions{}
}
