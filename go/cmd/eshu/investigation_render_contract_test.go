// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cli/investigation"
)

// TestInvestigationPacketFormatContract pins the --format flag mapping behind
// `eshu investigation export --format`, including the markdown alias and the
// operator-facing error for an unsupported format.
func TestInvestigationPacketFormatContract(t *testing.T) {
	cases := []struct {
		raw     string
		want    string
		wantErr string
	}{
		{raw: "", want: "json"},
		{raw: "json", want: "json"},
		{raw: "md", want: "md"},
		{raw: "markdown", want: "md"},
		{raw: "html", want: "html"},
		{raw: "yaml", wantErr: "unsupported packet format"},
	}
	for _, tc := range cases {
		format, err := investigation.ParsePacketFormat(tc.raw)
		if tc.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ParsePacketFormat(%q) error = %v, want %q", tc.raw, err, tc.wantErr)
			}
			continue
		}
		if err != nil {
			t.Fatalf("ParsePacketFormat(%q) error = %v, want nil", tc.raw, err)
		}
		if string(format) != tc.want {
			t.Fatalf("ParsePacketFormat(%q) = %q, want %q", tc.raw, string(format), tc.want)
		}
	}
}

// TestInvestigationRenderContract proves every supported format renders a
// packet deterministically through the CLI-owned render seam: JSON stays
// machine-readable, markdown and HTML stay human-readable views of the same
// packet, and an unknown format fails instead of emitting an empty artifact.
func TestInvestigationRenderContract(t *testing.T) {
	packet, err := investigation.BuildPacket(nil, investigation.DefaultDeps(), investigation.Request{
		Family:  investigation.ParseFamily("not_a_family"),
		Subject: map[string]string{"advisory_id": "GHSA-xxxx-yyyy-zzzz"},
	})
	if err != nil {
		t.Fatalf("BuildPacket() error = %v, want refusal packet instead", err)
	}

	render := func(format investigation.PacketFormat) []byte {
		t.Helper()
		data, err := investigation.RenderPacket(packet, format)
		if err != nil {
			t.Fatalf("RenderPacket(%q) error = %v, want nil", string(format), err)
		}
		if len(data) == 0 {
			t.Fatalf("RenderPacket(%q) = empty, want artifact bytes", string(format))
		}
		return data
	}

	jsonFormat, err := investigation.ParsePacketFormat("json")
	if err != nil {
		t.Fatalf("ParsePacketFormat(json) error = %v, want nil", err)
	}
	mdFormat, err := investigation.ParsePacketFormat("md")
	if err != nil {
		t.Fatalf("ParsePacketFormat(md) error = %v, want nil", err)
	}
	htmlFormat, err := investigation.ParsePacketFormat("html")
	if err != nil {
		t.Fatalf("ParsePacketFormat(html) error = %v, want nil", err)
	}

	jsonData := render(jsonFormat)
	var decoded map[string]any
	if err := json.Unmarshal(jsonData, &decoded); err != nil {
		t.Fatalf("RenderPacket(json) is not valid JSON: %v", err)
	}
	if decoded["schema"] != "investigation_evidence_packet.v2" {
		t.Fatalf("RenderPacket(json) schema = %v, want the v2 packet schema", decoded["schema"])
	}

	mdData := render(mdFormat)
	if !strings.Contains(string(mdData), "# Investigation Evidence Packet") {
		t.Fatalf("RenderPacket(md) is missing the packet heading")
	}
	if !strings.Contains(string(mdData), "GHSA-xxxx-yyyy-zzzz") {
		t.Fatalf("RenderPacket(md) is missing the requested scope")
	}

	htmlData := render(htmlFormat)
	if !strings.Contains(string(htmlData), "<html") {
		t.Fatalf("RenderPacket(html) is missing the document element")
	}
	if !strings.Contains(string(htmlData), "GHSA-xxxx-yyyy-zzzz") {
		t.Fatalf("RenderPacket(html) is missing the requested scope")
	}

	again, err := investigation.RenderPacket(packet, jsonFormat)
	if err != nil {
		t.Fatalf("RenderPacket(json) error = %v, want nil", err)
	}
	if string(again) != string(jsonData) {
		t.Fatal("RenderPacket(json) is not deterministic across calls")
	}
}
