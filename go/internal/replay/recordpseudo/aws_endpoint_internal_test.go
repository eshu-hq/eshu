// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAWSEndpointFormMirrorsTheGate: the region grammar and the fixed AWS
// word list of the endpoint allow form are the gate's, character for
// character, so the two cannot drift apart silently.
func TestAWSEndpointFormMirrorsTheGate(t *testing.T) {
	lib, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "scripts", "lib", "cassette_private_data_pattern.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{
		"aws_endpoint_region": "\tlocal aws_endpoint_region='" + awsEndpointRegion + "'\n",
		"aws_endpoint_words":  "\tlocal aws_endpoint_words='" + awsEndpointWords + "'\n",
	} {
		if !strings.Contains(string(lib), want) {
			t.Errorf("gate lib does not carry %s exactly as Verify does; want the line %q", name, want)
		}
	}
}

// TestAWSEndpointWordsAreTheKeptServiceLabels: the endpoint form's word
// list is exactly the service labels awsHostLabels keeps, so every label
// record mode keeps left of the tail fits the form and nothing else does.
func TestAWSEndpointWordsAreTheKeptServiceLabels(t *testing.T) {
	words := strings.Split(awsEndpointWords, "|")
	if len(words) != len(awsHostServiceLabels) {
		t.Fatalf("awsEndpointWords has %d words, awsHostServiceLabels %d", len(words), len(awsHostServiceLabels))
	}
	for _, word := range words {
		if _, ok := awsHostServiceLabels[word]; !ok {
			t.Errorf("awsEndpointWords carries %q, which awsHostLabels does not keep", word)
		}
	}
}
