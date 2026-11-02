// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ocr

import (
	"math/rand"
	"slices"
	"strings"
	"testing"
)

const (
	base64Alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	urlAlphabet    = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
)

// randomLines returns n deterministic lines of width characters drawn from
// alphabet, the shape of a PEM body or a wrapped token. The last line is
// shortened to tail characters when tail is positive, as a real body ends.
func randomLines(seed int64, alphabet string, n int, width int, tail int) []string {
	source := rand.New(rand.NewSource(seed))
	lines := make([]string, n)
	for i := range lines {
		size := width
		if i == n-1 && tail > 0 {
			size = tail
		}
		line := make([]byte, size)
		for j := range line {
			line[j] = alphabet[source.Intn(len(alphabet))]
		}
		lines[i] = string(line)
	}
	return lines
}

// multilineLayout is one screenshot layout whose secret spans several regions.
type multilineLayout struct {
	name    string
	regions []string
	secrets []string
	keep    []string
}

// multilineLayouts are the realistic shapes of a secret that runs past the
// two-region label window (#9799).
func multilineLayouts() []multilineLayout {
	rsa := randomLines(1, base64Alphabet, 26, 64, 44)
	openssh := randomLines(2, base64Alphabet, 12, 70, 36)
	wrapped := randomLines(3, urlAlphabet, 3, 40, 22)
	long := randomLines(4, urlAlphabet, 6, 32, 9)
	jwt := randomLines(5, urlAlphabet, 5, 48, 30)
	encrypted := randomLines(6, base64Alphabet, 18, 64, 20)
	column := randomLines(7, urlAlphabet, 5, 36, 14)
	armorless := randomLines(8, base64Alphabet, 12, 64, 60)
	return []multilineLayout{
		{
			name:    "RSA private key, armor and 26 body lines",
			regions: slices.Concat([]string{"-----BEGIN RSA PRIVATE KEY-----"}, rsa, []string{"-----END RSA PRIVATE KEY-----", "Rotate this key quarterly", "Review owner list", "Owner is the platform team"}),
			secrets: rsa,
			keep:    []string{"Owner is the platform team"},
		},
		{
			name:    "OpenSSH private key, armor and 12 body lines",
			regions: slices.Concat([]string{"-----BEGIN OPENSSH PRIVATE KEY-----"}, openssh, []string{"-----END OPENSSH PRIVATE KEY-----", "Deploy notes", "Review owner list", "Rollback steps"}),
			secrets: openssh,
			keep:    []string{"Rollback steps"},
		},
		{
			name:    "encrypted key with headers before the body",
			regions: slices.Concat([]string{"-----BEGIN RSA PRIVATE KEY-----", "Proc-Type: 4,ENCRYPTED", "DEK-Info: AES-256-CBC,9F3A5C7E1B2D4F60A8C0E2D4B6F8A0C2"}, encrypted, []string{"-----END RSA PRIVATE KEY-----", "Deploy notes", "Review owner list", "Rollback steps"}),
			secrets: encrypted,
			keep:    []string{"Rollback steps"},
		},
		{
			name:    "token wrapped across three lines under a label",
			regions: slices.Concat([]string{"API Token:"}, wrapped, []string{"Created by the platform team", "Rollback steps"}),
			secrets: wrapped,
			keep:    []string{"Rollback steps"},
		},
		{
			name:    "token wrapped across six lines with a caption between",
			regions: slices.Concat([]string{"Secret:", "copy the whole value"}, long, []string{"Rollback steps", "Ask the platform team"}),
			secrets: long,
			keep:    []string{"Ask the platform team"},
		},
		{
			name:    "bearer token wrapped across five lines",
			regions: slices.Concat([]string{"Authorization token"}, jwt, []string{"Release checklist", "Rollback steps", "Ask the platform team"}),
			secrets: jwt,
			keep:    []string{"Ask the platform team"},
		},
		{
			name:    "key body without its armor line",
			regions: slices.Concat([]string{"Release checklist", "Rollback steps"}, armorless, []string{"Ask the platform team"}),
			secrets: armorless,
			keep:    []string{"Release checklist", "Rollback steps", "Ask the platform team"},
		},
		{
			name:    "wrapped value in the column beside its label",
			regions: slices.Concat([]string{"Token:"}, column, []string{"Rollback steps", "Ask the platform team"}),
			secrets: column,
			keep:    []string{"Ask the platform team"},
		},
	}
}

// leakedSecrets returns the secret lines that still appear in persisted
// content.
func leakedSecrets(contents []string, secrets []string) []string {
	leaked := []string{}
	for _, secret := range secrets {
		for _, content := range contents {
			if strings.Contains(content, secret) {
				leaked = append(leaked, secret)
				break
			}
		}
	}
	return leaked
}

func TestExtractWithholdsASecretThatSpansManyLines(t *testing.T) {
	t.Parallel()

	for _, layout := range multilineLayouts() {
		t.Run(layout.name, func(t *testing.T) {
			t.Parallel()
			result := extractRegions(t, Options{}, stackedRegions(layout.regions...))
			contents := persistedContents(result)
			if leaked := leakedSecrets(contents, layout.secrets); len(leaked) > 0 {
				t.Errorf("%d of %d secret lines persisted in clear: first %q", len(leaked), len(layout.secrets), leaked[0])
			}
			for _, keep := range layout.keep {
				if !slices.Contains(contents, keep) {
					t.Errorf("ordinary line %q was withheld, want kept", keep)
				}
			}
		})
	}
}
