// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/replay/recordpseudo"
)

// The planted account is the gate's own synthetic sample.
const plantedAccount = "987654321098"

// committedCassettes lists every committed cassette file.
func committedCassettes(t *testing.T) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(corpusDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(path, ".json") {
			out = append(out, path)
		}
		return nil
	})
	if err != nil || len(out) == 0 {
		t.Fatalf("walk %s: %v (%d files)", corpusDir, err, len(out))
	}
	return out
}

// TestVerifyAgreesWithGateOnCommittedCorpus: the committed cassettes pass the
// gate, so Verify must accept every one of them with no produced set --
// the allow forms in verify_forms.go mirror the gate's.
func TestVerifyAgreesWithGateOnCommittedCorpus(t *testing.T) {
	for _, path := range committedCassettes(t) {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := recordpseudo.Verify(raw, recordpseudo.Set{}); err != nil {
			t.Errorf("%s: %v", path, err)
		}
	}
}

// TestVerifySeededRedGreen is the guard's RED/GREEN pair: a clean recorded
// cassette passes (GREEN); the same bytes with one raw account planted in an
// unclassified position are refused, naming the alternative and offset and
// never the value (RED).
func TestVerifySeededRedGreen(t *testing.T) {
	clean := recordBytes(t, filepath.Join(t.TempDir(), "clean.json"), committedSource(t, "awscloud"), mustKey(t, keyA))
	if err := recordpseudo.Verify(clean, recordpseudo.Set{}); err == nil {
		t.Fatal("GREEN control is wrong: a pseudonymized cassette passed Verify with an EMPTY produced set, so the 0000 form is not membership-checked")
	}
	produced := producedSet(t, clean)
	if err := recordpseudo.Verify(clean, produced); err != nil {
		t.Fatalf("GREEN: clean output refused: %v", err)
	}
	plants := map[string]string{
		"account12": `"note":"` + plantedAccount + `"`,
		"arn":       `"note":"arn:aws:iam::` + plantedAccount + `:role/example"`,
		"ipv4":      `"note":"10.20.30.40"`,
		"hostname":  `"note":"vault.org-demo.corp"`,
		"ipv6":      `"note":"fd00:1234::1"`,
		"nodeip":    `"note":"ip-10-20-30-40"`,
	}
	for alternative, plant := range plants {
		planted := strings.Replace(string(clean), `"schema_version":`, plant+`,"schema_version":`, 1)
		if planted == string(clean) {
			t.Fatalf("plant for %s did not land; the canonical form changed", alternative)
		}
		err := recordpseudo.Verify([]byte(planted), produced)
		if err == nil {
			t.Errorf("RED %s: planted raw value passed Verify", alternative)
			continue
		}
		if !strings.Contains(err.Error(), "alternative "+alternative) {
			t.Errorf("RED %s: refusal does not name the alternative: %v", alternative, err)
		}
		for _, value := range []string{plantedAccount, "10.20.30.40", "org-demo", "fd00:1234"} {
			if strings.Contains(err.Error(), value) {
				t.Errorf("RED %s: the refusal printed a value", alternative)
			}
		}
	}
}

// TestVerifyReservedAccountNeedsMembership: the 0000xxxxxxxx form passes
// the gate by shape but Verify admits it only when this run minted it.
func TestVerifyReservedAccountNeedsMembership(t *testing.T) {
	doc := []byte(`{"account_id":"000012345678","arn":"arn:aws:iam::000012345678:role/x","host":"000012345678.dkr.ecr.us-east-1.amazonaws.com"}`)
	if err := recordpseudo.Verify(doc, recordpseudo.Set{}); err == nil {
		t.Fatal("unproduced reserved account passed")
	} else if !strings.Contains(err.Error(), "5 candidate") {
		// Three account12 hits (bare, inside the ARN, the ECR host label --
		// the gate counts all three), one arn, one hostname.
		t.Errorf("want 5 candidates refused: %v", err)
	}
	if err := recordpseudo.Verify(doc, recordpseudo.Set{"000012345678": {}}); err != nil {
		t.Fatalf("produced reserved account refused: %v", err)
	}
	// A raw account that happens to start with 0000 is the residual the
	// belt exists for: not produced, therefore refused.
	if err := recordpseudo.Verify([]byte(`{"a":"000198765432"}`), recordpseudo.Set{"000012345678": {}}); err == nil {
		t.Fatal("a 0000-prefixed raw account that this run did not mint passed")
	}
}

// TestVerifyDocumentationFormsPass mirrors the gate's negative controls.
func TestVerifyDocumentationFormsPass(t *testing.T) {
	doc := []byte(`{"a":"123456789012","b":"000000000001","c":"555555555555","d":"arn:aws:s3:::example-bucket","e":"arn:aws:iam::aws:policy/example","f":"192.0.2.10","g":"127.0.0.1","h":"2001:db8::1","i":"::1","j":"00:00:5e:00:53:0a","k":"registry.example.com","l":"github.com","m":"compute.googleapis.com","n":"123456789012.dkr.ecr.us-east-1.amazonaws.com","o":"vault.supply-chain-demo.internal","p":"ip-192-0-2-10","q":"sha256:0e0f26e6dce79a7c164729766618cb750eca10c8b92f9c22","r":"aws_s3_bucket.local_backend_demo","s":"1.2.3.4.5","t":"h1a2b3c4d5e.h0f9e8d7c6b.example"}`)
	if err := recordpseudo.Verify(doc, recordpseudo.Set{}); err != nil {
		t.Fatalf("documentation forms refused: %v", err)
	}
}

// producedSet reads the recorded cassette's own pseudonyms back: every
// 0000-account in it was minted by that run.
func producedSet(t *testing.T, canonical []byte) recordpseudo.Set {
	t.Helper()
	set := recordpseudo.Set{}
	text := string(canonical)
	for i := 0; i+12 <= len(text); i++ {
		if strings.HasPrefix(text[i:], "0000") && (i == 0 || !isHexByte(text[i-1])) && (i+12 == len(text) || !isHexByte(text[i+12])) {
			candidate := text[i : i+12]
			if strings.Trim(candidate, "0123456789") == "" {
				set[candidate] = struct{}{}
			}
		}
	}
	if len(set) == 0 {
		t.Fatal("no reserved-form account in the recorded cassette")
	}
	return set
}

func isHexByte(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}
