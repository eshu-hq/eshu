// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package documentationexport

import (
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/exportmanifestpreflight"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Request describes one explicit offline documentation export parsing attempt.
type Request struct {
	ScopeID      string
	GenerationID string
	ObservedAt   time.Time
	ManifestName string
	Manifest     []byte
	Files        map[string][]byte
}

// Result contains preflight evidence and any source-neutral documentation facts.
type Result struct {
	Preflight exportmanifestpreflight.Result
	Envelopes []facts.Envelope
}

type manifest struct {
	SourceSystem    string         `json:"source_system"`
	SourceScopeID   string         `json:"source_scope_id"`
	SourceScopeKind string         `json:"source_scope_kind"`
	ExportedAt      string         `json:"exported_at"`
	SourceRevision  string         `json:"source_revision"`
	SourceCursor    string         `json:"source_cursor"`
	ACLPolicy       string         `json:"acl_policy"`
	Files           []manifestFile `json:"files"`
	Metadata        manifestMeta   `json:"metadata"`
}

type manifestFile struct {
	Path           string `json:"path"`
	Kind           string `json:"kind"`
	ContentType    string `json:"content_type"`
	SourceItemID   string `json:"source_item_id"`
	URL            string `json:"url"`
	Deleted        bool   `json:"deleted"`
	Edited         bool   `json:"edited"`
	PrivateChannel bool   `json:"private_channel"`
}

type manifestMeta struct {
	PrivateChannel bool `json:"private_channel"`
}

type exportRecord struct {
	ID        string          `json:"id"`
	Title     string          `json:"title"`
	Body      string          `json:"body"`
	Text      string          `json:"text"`
	Content   string          `json:"content"`
	Deleted   bool            `json:"deleted"`
	Edited    bool            `json:"edited"`
	Comments  []recordSection `json:"comments"`
	Timeline  []recordSection `json:"timeline"`
	Changelog []recordSection `json:"changelog"`
	Messages  []recordSection `json:"messages"`
	Links     []recordLink    `json:"links"`
}

type recordSection struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Heading string `json:"heading"`
	Body    string `json:"body"`
	Text    string `json:"text"`
	Content string `json:"content"`
	Deleted bool   `json:"deleted"`
	Edited  bool   `json:"edited"`
}

type recordLink struct {
	ID        string `json:"id"`
	SectionID string `json:"section_id"`
	Target    string `json:"target"`
	TargetURI string `json:"target_uri"`
	URL       string `json:"url"`
	Anchor    string `json:"anchor"`
}
