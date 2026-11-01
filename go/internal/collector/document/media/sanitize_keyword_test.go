// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package media

import "testing"

// TestContainsSensitiveKeywordSpokenForms pins the #9788 rule: the transcript
// keyword check folds a segment (case, compatibility forms, diacritics,
// invisible format characters, a bounded homoglyph set, separator runs) before
// it matches, and a few short or ambiguous words match only as whole words.
func TestContainsSensitiveKeywordSpokenForms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want bool
	}{
		// Keywords the rule already matched.
		{"password", "The password is", true},
		{"password uppercase", "THE PASSWORD IS", true},
		{"password plural", "the passwords are", true},
		{"api_key", "The api_key is", true},
		{"private_key", "the private_key is", true},
		{"credential_marker", "credential_marker gateway-api", true},
		{"secret_marker", "secret_marker was spoken", true},
		{"token_marker", "token_marker-service", true},

		// Separator variants of password.
		{"pass word space", "The pass word is", true},
		{"pass-word hyphen", "The pass-word is", true},
		{"pass_word underscore", "The pass_word is", true},
		{"pass.word dot", "The pass.word is", true},
		{"pass word two spaces", "The pass  word is", true},
		{"Pass Word capitals", "The Pass Word is", true},
		{"pass words plural", "the pass words are", true},

		// Spoken forms of the existing key keywords.
		{"api key spoken", "The API key is", true},
		{"api-key hyphen", "the api-key is", true},
		{"apikey joined", "the apikey is", true},
		{"private key spoken", "The private key is", true},
		{"private-key hyphen", "the private-key is", true},

		// Synonyms.
		{"passphrase", "The passphrase is", true},
		{"pass phrase", "The pass phrase is", true},
		{"pass-phrase", "The pass-phrase is", true},
		{"passcode", "The passcode is", true},
		{"pass code", "The pass code is", true},
		{"passwd", "set passwd to", true},
		{"pwd", "the pwd is", true},
		{"PWD uppercase", "the PWD is", true},
		{"credential", "the credential is", true},
		{"credentials", "the credentials are", true},
		{"token", "The token is", true},
		{"tokens", "the tokens are", true},
		{"token possessive", "the token's value is", true},
		{"token then punctuation", "token: abc", true},
		{"bearer token", "The bearer token is", true},
		{"PIN", "The PIN is", true},
		{"pin lowercase", "the pin is", true},
		{"pin code", "the pin code is", true},
		{"pin before digits", "pin 4 8 2 1", true},
		{"P.I.N. spelled", "the P.I.N. is", true},
		{"p i n spelled", "the p i n is", true},
		{"P-I-N spelled", "the P-I-N is", true},

		// Evasions of the match, not of the speech.
		{"soft hyphen", "The pass\u00adword is", true},
		{"zero width space", "The pass\u200bword is", true},
		{"zero width joiner", "The pass\u200dword is", true},
		{"byte order mark", "The pass\ufeffword is", true},
		{"word joiner", "The pass\u2060word is", true},
		{"cyrillic a", "The p\u0430ssword is", true},
		{"cyrillic o", "The passw\u043erd is", true},
		{"cyrillic capital A", "The P\u0410SSWORD is", true},
		{"greek omicron", "The passw\u03bfrd is", true},
		{"cyrillic p in pin", "the \u0440in is", true},
		{"cyrillic k in api key", "the api \u043aey is", true},
		{"fullwidth", "The \uff50\uff41\uff53\uff53\uff57\uff4f\uff52\uff44 is", true},
		{"combining accent", "The pa\u0301ssword is", true},
		{"precomposed accent", "The p\u00e1ssword is", true},
		{"no-break space separator", "The pass\u00a0word is", true},
		{"ideographic space separator", "The pass\u3000word is", true},
		{"soft hyphen in token", "The tok\u00aden is", true},

		// Ordinary words stay clear.
		{"empty", "", false},
		{"ordinary speech", "Restart the payments worker.", false},
		{"tokenize", "we tokenize the input", false},
		{"tokenizer", "the tokenizer is slow", false},
		{"tokenization", "tokenization runs first", false},
		{"detokenize", "then detokenize it", false},
		{"pinned", "the version is pinned", false},
		{"pinning", "we are pinning it", false},
		{"spinning", "the fan is spinning", false},
		{"shipping", "shipping on Friday", false},
		{"mapping", "the mapping is stale", false},
		{"pins", "the pins are bent", false},
		{"pint", "a pint of milk", false},
		{"passage", "the passage was long", false},
		{"passenger", "one passenger waited", false},
		{"compass word", "the compass word", false},
		{"bypass word", "bypass wording", false},
		{"pass then other word", "pass the salt", false},
		{"pass key not a keyword", "pass the key", false},
		{"compass", "follow the compass", false},
		{"token inside longer word", "the tokenless path", false},
		{"pwd inside text is rare but the word alone", "print working directory", false},
		{"api alone", "the api is slow", false},
		{"key alone", "the key is on the desk", false},
		{"private alone", "a private meeting", false},
		{"credit", "the credit is applied", false},
		{"p i n inside words", "up in the air", false},
		{"letters apart", "p is in n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := containsSensitiveKeyword(tt.text); got != tt.want {
				t.Fatalf("containsSensitiveKeyword(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

// TestSpokenKeywordRuleStaysOutOfIdentityAndHints pins that the wider spoken
// rule is for transcript segments only. A source name or a mention hint such as
// "token-service" is a label, not speech, and keeps the narrower rule.
func TestSpokenKeywordRuleStaysOutOfIdentityAndHints(t *testing.T) {
	t.Parallel()

	for _, text := range []string{"docs/token-rotation.wav", "pin-assignments", "token-service", "pass word.wav"} {
		if containsSensitiveMarker(text) {
			t.Fatalf("containsSensitiveMarker(%q) = true, want false", text)
		}
		if got, redacted := safeIdentity(text, "x"); redacted || got != text {
			t.Fatalf("safeIdentity(%q) = %q, %v, want unchanged", text, got, redacted)
		}
	}
	for _, text := range []string{"docs/password-reset.wav", "api_key-rotation", "token_marker-service"} {
		if !containsSensitiveMarker(text) {
			t.Fatalf("containsSensitiveMarker(%q) = false, want true (legacy rule)", text)
		}
	}
}

// TestValueWindowOpensOnSpokenForms proves each new form arms a value window
// end to end through valueSegments, not only through the keyword check.
func TestValueWindowOpensOnSpokenForms(t *testing.T) {
	t.Parallel()

	for _, text := range []string{
		"The pass word is", "The pass-word is", "The passphrase is", "The passcode is",
		"The pwd is", "The PIN is", "The token is", "The credential is", "The API key is",
		"The p\u0430ssword is", "The pass\u00adword is",
	} {
		got := valueSegments(sequentialSegments(text, "swordfish.", "Restart the worker."))
		want := []bool{false, true, true}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("valueSegments(%q) = %v, want %v", text, got, want)
			}
		}
	}
}
