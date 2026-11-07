// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package investigation_test

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cli/investigation"
)

// TestParsePacketFormatContract pins the format strings the CLI accepts. The
// seam delegates to the packet contract, so these cases prove the delegation
// preserves the accepted spellings, the markdown alias, and the refusal of an
// unknown format.
func TestParsePacketFormatContract(t *testing.T) {
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
		{raw: "  ", want: "json"},
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

// TestRenderPacketRefusesUnknownFormat proves an unsupported format fails
// instead of emitting an empty artifact.
func TestRenderPacketRefusesUnknownFormat(t *testing.T) {
	packet, err := investigation.BuildPacket(nil, investigation.DefaultDeps(), investigation.Request{
		Family:  investigation.ParseFamily("not_a_family"),
		Subject: map[string]string{"requested": "unspecified"},
	})
	if err != nil {
		t.Fatalf("BuildPacket() error = %v, want refusal packet instead", err)
	}
	if _, err := investigation.RenderPacket(packet, investigation.PacketFormat("yaml")); err == nil {
		t.Fatal("RenderPacket(yaml) error = nil, want unsupported-format failure")
	} else if !strings.Contains(err.Error(), "unsupported packet format") {
		t.Fatalf("RenderPacket(yaml) error = %q, want unsupported-format text", err.Error())
	}
}
