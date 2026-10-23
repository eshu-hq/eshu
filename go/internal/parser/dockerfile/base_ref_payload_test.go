// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dockerfile

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

// argFanOutSource declares args global ARGs whose default is a platform ARG
// reference plus padLen bytes (so none is known), then repeats a FROM line that
// reads every one of them stages times. Without a bound on the evidence, each
// stage row would repeat every raw default.
func argFanOutSource(args, stages, padLen int) string {
	var source strings.Builder
	pad := strings.Repeat("q", padLen)
	refs := make([]string, 0, args)
	for i := 0; i < args; i++ {
		fmt.Fprintf(&source, "ARG U%d=${TARGETARCH}%s\n", i, pad)
		refs = append(refs, fmt.Sprintf("$U%d", i))
	}
	for i := 0; i < stages; i++ {
		fmt.Fprintf(&source, "FROM i:%s\n", strings.Join(refs, ""))
	}
	return source.String()
}

// TestBaseRefArgDefaultEvidencePayloadStaysBounded marshals the payload the
// fact pipeline and the API response carry. 200 ARGs of about 4 KB each read by
// 1000 FROM lines (1.7 MB of input) marshaled to 808 MB before the evidence was
// capped. The remaining payload is the input itself: the ARG rows hold every
// default once, and each stage row holds its own FROM text and name list.
func TestBaseRefArgDefaultEvidencePayloadStaysBounded(t *testing.T) {
	t.Parallel()

	// Largest legal stage evidence: maxArgDefaultsPerStage entries of
	// maxArgDefaultLen bytes, each with a name and JSON punctuation.
	const perStageEvidence = maxArgDefaultsPerStage * (maxArgDefaultLen + 64)
	cases := []struct {
		name       string
		stages     int
		maxPayload int
	}{
		{"200 ARGs, 100 FROM lines", 100, 2 << 20},
		{"200 ARGs, 1000 FROM lines", 1000, 10 << 20},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			source := argFanOutSource(200, tc.stages, 4000)
			metadata := RuntimeMetadata(source)
			body, err := json.Marshal(metadata.Map())
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if len(body) > tc.maxPayload {
				t.Fatalf("payload is %d bytes for %d bytes of input, want at most %d", len(body), len(source), tc.maxPayload)
			}
			for _, stage := range metadata.Stages {
				evidence, err := json.Marshal(stage.ArgDefaults)
				if err != nil {
					t.Fatalf("marshal evidence: %v", err)
				}
				if len(evidence) > perStageEvidence {
					t.Fatalf("stage on line %d carries %d bytes of ARG default evidence, want at most %d",
						stage.LineNumber, len(evidence), perStageEvidence)
				}
			}
		})
	}
}

func TestBaseRefArgDefaultIsCutAtTheValueCapAndMarked(t *testing.T) {
	t.Parallel()

	long := "${TARGETARCH}" + strings.Repeat("a", 3*maxArgDefaultLen)
	stage := stageAtLine(t, RuntimeMetadata("ARG LONG="+long+"\nARG SHORT=${TARGETARCH}-x\nFROM i:$LONG$SHORT\n"), 3)
	if got := len(stage.ArgDefaults["LONG"]); got != maxArgDefaultLen {
		t.Fatalf("stored LONG default has %d bytes, want %d", got, maxArgDefaultLen)
	}
	if !strings.HasPrefix(long, stage.ArgDefaults["LONG"]) {
		t.Fatalf("stored LONG default is not a prefix of the declared default")
	}
	if stage.ArgDefaults["SHORT"] != "${TARGETARCH}-x" {
		t.Fatalf("SHORT default = %q, want it stored whole", stage.ArgDefaults["SHORT"])
	}
	if !equalStrings(stage.ArgDefaultsTruncated, []string{"LONG"}) {
		t.Fatalf("ArgDefaultsTruncated = %v, want [LONG] only", stage.ArgDefaultsTruncated)
	}
	if stage.ArgDefaultsOmitted {
		t.Fatalf("ArgDefaultsOmitted is set with two defaults, want unset")
	}
}

func TestBaseRefArgDefaultCutKeepsValidUTF8(t *testing.T) {
	t.Parallel()

	// A 3-byte rune that straddles the cap must be dropped whole.
	value := "${TARGETARCH}" + strings.Repeat("a", maxArgDefaultLen-len("${TARGETARCH}")-1) + "€€"
	stage := stageAtLine(t, RuntimeMetadata("ARG V="+value+"\nFROM i:$V\n"), 2)
	stored := stage.ArgDefaults["V"]
	if len(stored) > maxArgDefaultLen || !utf8Valid(stored) {
		t.Fatalf("stored default has %d bytes, valid=%v, want at most %d bytes of valid UTF-8", len(stored), utf8Valid(stored), maxArgDefaultLen)
	}
	if !equalStrings(stage.ArgDefaultsTruncated, []string{"V"}) {
		t.Fatalf("ArgDefaultsTruncated = %v, want [V]", stage.ArgDefaultsTruncated)
	}
}

func TestBaseRefArgDefaultEntriesAreCappedPerStage(t *testing.T) {
	t.Parallel()

	total := maxArgDefaultsPerStage + 4
	var source strings.Builder
	refs := make([]string, 0, total)
	for i := 0; i < total; i++ {
		fmt.Fprintf(&source, "ARG A%02d=${TARGETARCH}\n", i)
		refs = append(refs, fmt.Sprintf("$A%02d", i))
	}
	source.WriteString("FROM i:" + strings.Join(refs, "-") + "\n")
	stage := stageAtLine(t, RuntimeMetadata(source.String()), total+1)
	if len(stage.ArgDefaults) != maxArgDefaultsPerStage {
		t.Fatalf("stored %d defaults, want %d", len(stage.ArgDefaults), maxArgDefaultsPerStage)
	}
	for i := 0; i < maxArgDefaultsPerStage; i++ {
		if _, ok := stage.ArgDefaults[fmt.Sprintf("A%02d", i)]; !ok {
			t.Fatalf("default A%02d is missing, want the first names in sorted order kept", i)
		}
	}
	if !stage.ArgDefaultsOmitted {
		t.Fatalf("ArgDefaultsOmitted is unset, want the omission visible")
	}
	if len(stage.UnresolvedArgs) != total {
		t.Fatalf("UnresolvedArgs has %d names, want all %d still listed", len(stage.UnresolvedArgs), total)
	}
}

func TestBaseRefArgDefaultsAtTheCapAreNotMarked(t *testing.T) {
	t.Parallel()

	var source strings.Builder
	refs := make([]string, 0, maxArgDefaultsPerStage)
	for i := 0; i < maxArgDefaultsPerStage; i++ {
		fmt.Fprintf(&source, "ARG A%02d=${TARGETARCH}%s\n", i, strings.Repeat("a", maxArgDefaultLen-len("${TARGETARCH}")))
		refs = append(refs, fmt.Sprintf("$A%02d", i))
	}
	source.WriteString("FROM i:" + strings.Join(refs, "-") + "\n")
	got := RuntimeMetadata(source.String())
	stage := stageAtLine(t, got, maxArgDefaultsPerStage+1)
	if len(stage.ArgDefaults) != maxArgDefaultsPerStage || stage.ArgDefaultsOmitted || len(stage.ArgDefaultsTruncated) != 0 {
		t.Fatalf("defaults=%d omitted=%v truncated=%v, want %d whole defaults and no marker",
			len(stage.ArgDefaults), stage.ArgDefaultsOmitted, stage.ArgDefaultsTruncated, maxArgDefaultsPerStage)
	}
	row := got.Map()["dockerfile_stages"].([]map[string]any)[0]
	for _, key := range []string{"base_ref_arg_defaults_truncated", "base_ref_arg_defaults_omitted"} {
		if _, present := row[key]; present {
			t.Fatalf("%s is present for a stage with nothing cut", key)
		}
	}
}

func TestBaseRefStageLocalArgDefaultsAreCapped(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("b", 3*maxArgDefaultLen)
	var source strings.Builder
	source.WriteString("FROM i:$LONG$ZZ\n")
	source.WriteString("ARG LONG=" + long + "\n")
	source.WriteString("ARG ZZ=ok\n")
	stage := stageAtLine(t, RuntimeMetadata(source.String()), 1)
	if got := len(stage.ArgDefaults["LONG"]); got != maxArgDefaultLen {
		t.Fatalf("stage-local LONG default has %d bytes, want %d", got, maxArgDefaultLen)
	}
	if stage.ArgDefaults["ZZ"] != "ok" || !equalStrings(stage.ArgDefaultsTruncated, []string{"LONG"}) {
		t.Fatalf("ZZ=%q truncated=%v, want ZZ stored whole and only LONG marked", stage.ArgDefaults["ZZ"], stage.ArgDefaultsTruncated)
	}
}

func TestBaseRefArgDefaultCapDoesNotChangeResolution(t *testing.T) {
	t.Parallel()

	// A known default longer than the evidence cap still expands in full: the
	// cap bounds the stored evidence, never the value a FROM resolves to.
	value := strings.Repeat("v", 2*maxArgDefaultLen)
	stage := stageAtLine(t, RuntimeMetadata("ARG V="+value+"\nFROM ${V}\n"), 2)
	if len(stage.UnresolvedArgs) != 0 || stage.BaseImage != value {
		t.Fatalf("unresolved=%v image has %d bytes, want it resolved to the full %d-byte value",
			stage.UnresolvedArgs, len(stage.BaseImage), len(value))
	}
}

func TestBaseRefMapCarriesTheEvidenceMarkers(t *testing.T) {
	t.Parallel()

	long := "${TARGETARCH}" + strings.Repeat("a", 2*maxArgDefaultLen)
	var source strings.Builder
	source.WriteString("ARG A_LONG=" + long + "\n")
	refs := []string{"$A_LONG"}
	for i := 0; i < maxArgDefaultsPerStage; i++ {
		fmt.Fprintf(&source, "ARG B%02d=${TARGETARCH}\n", i)
		refs = append(refs, fmt.Sprintf("$B%02d", i))
	}
	source.WriteString("FROM i:" + strings.Join(refs, "-") + "\n")
	row := RuntimeMetadata(source.String()).Map()["dockerfile_stages"].([]map[string]any)[0]
	if row["base_ref_arg_defaults_omitted"] != true {
		t.Fatalf("base_ref_arg_defaults_omitted = %v, want true", row["base_ref_arg_defaults_omitted"])
	}
	marked, _ := row["base_ref_arg_defaults_truncated"].([]any)
	if len(marked) != 1 || marked[0] != "A_LONG" {
		t.Fatalf("base_ref_arg_defaults_truncated = %#v, want [A_LONG]", row["base_ref_arg_defaults_truncated"])
	}
}

// TestBaseRefDeclareKeepsNoValueForAnUnknownDefault pins a hygiene property:
// lookup already ignores a global that is not known, and declare also never
// stores the expanded text of one.
func TestBaseRefDeclareKeepsNoValueForAnUnknownDefault(t *testing.T) {
	t.Parallel()

	resolver := newBaseRefResolver()
	resolver.declare(argDecl{name: "BIG", value: strings.Repeat("x", maxExpandedLen+1), hasDefault: true})
	resolver.declare(argDecl{name: "ARCH", value: "${TARGETARCH}-x", hasDefault: true})
	for _, name := range []string{"BIG", "ARCH"} {
		global := resolver.globals[name]
		if global.known || global.value != "" {
			t.Fatalf("%s: known=%v value has %d bytes, want an unknown global with no stored value", name, global.known, len(global.value))
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func utf8Valid(s string) bool { return utf8.ValidString(s) }
