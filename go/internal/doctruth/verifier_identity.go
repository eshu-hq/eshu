// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package doctruth

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"
)

func defaultGenerationID(now time.Time) string {
	var randomBytes [12]byte
	if _, err := rand.Read(randomBytes[:]); err == nil {
		return "documentation-verify:" + now.UTC().Format(time.RFC3339Nano) + ":" + hex.EncodeToString(randomBytes[:])
	}
	return "documentation-verify:" + time.Now().UTC().Format(time.RFC3339Nano)
}

func (v *Verifier) version() string {
	return v.now().UTC().Format(time.RFC3339Nano) + "#" + v.generationID
}

func canonicalDocumentURI(sourceSystem string, doc DocumentInput) string {
	if uri := strings.TrimSpace(doc.SourceURI); uri != "" {
		return uri
	}
	return strings.TrimSpace(sourceSystem) + ":" + strings.TrimSpace(doc.Path)
}
