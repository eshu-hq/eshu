// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package media

import (
	"context"
	"testing"
)

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
		{"embedded api_key", "xapi_key=abc", true},
		{"embedded private_key", "myprivate_key", true},
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

		// F1: secret as a whole word. Fails closed on "a secret garden" on purpose.
		{"secret", "The secret is", true},
		{"secrets", "the secrets are", true},
		{"secret key", "the secret key is", true},
		{"client secret", "The client secret is", true},
		{"secret garden fails closed", "a secret garden", true},
		{"secretary stays clear", "the secretary called", false},
		{"secretive stays clear", "a secretive plan", false},
		{"secretion stays clear", "the secretion rate", false},
		{"unsecret is not a word", "an insecret", false},

		// F4: comma and spelled-out forms.
		{"P, I, N", "the P, I, N is", true},
		{"p,i,n", "the p,i,n is", true},
		{"A.P.I. key", "the A.P.I. key is", true},
		{"A P I key", "the A P I key is", true},
		{"A-P-I key", "the A-P-I key is", true},
		{"P-W-D", "the P-W-D is", true},
		{"P W D", "the P W D is", true},
		{"pincode", "the pincode is", true},
		{"PINcode", "the PINcode is", true},
		{"pass, word", "the pass, word is", true},
		{"upwd not p w d", "up w dishes", false},

		// F5: capital homoglyphs fold to the letter they look like.
		{"greek capital rho iota nu", "the \u03a1\u0399\u039d is", true},
		{"greek capital token with nu", "the TOKE\u039d is", true},
		{"greek capital key with upsilon", "the api KE\u03a5 is", true},
		{"greek eta as n", "the pi\u03b7 is", true},
		{"cyrillic pe as n", "the pi\u043f is", true},
		{"cyrillic palochka as l", "the credentia\u04cf is", true},
		{"cyrillic ha as h", "the pass p\u04bbrase is", true},
		{"small nu stays v", "the pri\u03bdate key is", true},

		// R1: capital look-alikes of h. Cyrillic en and Greek eta look like H.
		{"cyrillic small en as h", "the passp\u043drase is", true},
		{"cyrillic capital en as h", "the PASSP\u041dRASE is", true},
		{"greek capital eta as h", "the PASSP\u0397RASE is", true},
		{"greek small eta still n", "the pi\u03b7 is", true},

		// R2: access key and privatekey.
		{"access key", "The access key is", true},
		{"access_key underscore", "the access_key is", true},
		{"access-key", "the access-key is", true},
		{"AccessKey", "the AccessKey is", true},
		{"accesskey", "the accesskey is", true},
		{"access keys", "the access keys are", true},
		{"AWS access key id", "the AWS access key id is", true},
		{"privatekey joined", "the privatekey is", true},
		{"PrivateKey joined", "the PrivateKey is", true},
		{"MyAccessKey embedded", "the MyAccessKey is", true},
		{"embedded accesskey", "the myaccesskey is", true},
		{"access keyboard stays clear", "the access keyboard layout", false},
		{"accessible keyboard stays clear", "an accessible keyboard", false},
		{"access to the key stays clear", "access to the key", false},

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
		{"betoken", "it does betoken trouble", false},
		{"spin", "give it a spin", false},
		{"letters apart", "p is in n", false},

		// A rejected occurrence must not hide a later accepted one.
		{"rejected pin then accepted pin", "the spinning fan, pin 1234", true},
		{"compass word then pass word", "compass word, the pass word is", true},
		{"tokenize then token", "tokenize it, the token is", true},
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
		got, _ := valueSegments(sequentialSegments(text, "swordfish.", "Restart the worker."))
		want := []bool{false, true, true}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("valueSegments(%q) = %v, want %v", text, got, want)
			}
		}
	}
}

// TestExtractRedactsSpokenKeywordSegmentAndValue runs the new forms through
// Extract: the keyword segment redacts itself and its value, and the engine's
// ordinary speech after the window keeps its text.
func TestExtractRedactsSpokenKeywordSegmentAndValue(t *testing.T) {
	t.Parallel()

	for _, text := range []string{"The PIN is", "The pass word is", "The p\u0430ssword is", "The token is"} {
		engine := &fakeTranscriptEngine{result: EngineResult{
			Segments: sequentialSegments(text, "swordfish.", "hunter2.", "Restart the worker."),
		}}
		result, err := Extract(context.Background(), testRequest("docs/standup.wav", encodeTestWAV(t, 5000), engine))
		if err != nil {
			t.Fatalf("Extract(%q) error = %v", text, err)
		}
		want := []bool{true, true, true, false}
		if len(result.Sections) != len(want) {
			t.Fatalf("Extract(%q) sections = %d, want %d", text, len(result.Sections), len(want))
		}
		for i, section := range result.Sections {
			assertSectionRedaction(t, i, section, want[i])
		}
		assertAbsentFromEnvelopes(t, result, "swordfish")
	}
}

// TestValueWindowOpensOnKeywordSplitAcrossSegments pins the #9788 review
// finding: a keyword the engine splits across two segments still opens a
// window, at the second segment, in both walk orders.
func TestValueWindowOpensOnKeywordSplitAcrossSegments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		segments []Segment
		want     []bool
	}{
		{
			"api then key", sequentialSegments("The API", "key is", "swordfish.", "hunter2.", "Restart the worker."),
			[]bool{false, true, true, true, false},
		},
		{
			"pass then word", sequentialSegments("The pass", "word is", "swordfish.", "hunter2.", "Restart the worker."),
			[]bool{false, true, true, true, false},
		},
		{
			"private then key", sequentialSegments("my private", "key is", "swordfish.", "hunter2.", "Restart the worker."),
			[]bool{false, true, true, true, false},
		},
		{
			"letters spelled across", sequentialSegments("The P I", "N is", "swordfish.", "hunter2.", "Restart the worker."),
			[]bool{false, true, true, true, false},
		},
		{
			"blank segment between halves", sequentialSegments("The API", "  ", "key is", "swordfish.", "hunter2.", "ok."),
			[]bool{false, false, true, true, true, false},
		},
		{
			"unrelated pair stays clear", sequentialSegments("Follow the compass", "word is out", "Restart the worker."),
			[]bool{false, false, false},
		},
		{
			"key alone stays clear", sequentialSegments("The deploy", "key is on the desk", "Restart the worker."),
			[]bool{false, false, false},
		},
		{"out of order, slice order pair", []Segment{
			timedSegment("key is", 2000), timedSegment("The API", 1000), timedSegment("swordfish.", 3000),
		}, []bool{true, false, true}},
		{
			"pair only in slice order",
			[]Segment{
				timedSegment("The API", 3000), timedSegment("key is", 1000), timedSegment("swordfish.", 2000),
			},
			[]bool{false, true, true},
		},
		{
			"keyword alone does not also straddle", sequentialSegments("The api key", "key is", "swordfish.", "hunter2.", "Restart the worker."),
			[]bool{false, true, true, false, false},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, _ := valueSegments(tt.segments)
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d", len(got), len(tt.want))
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("valueSegments = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

// TestFoldSpokenCapitalLookalikes pins the letter each capital look-alike
// folds to. Greek capital eta and Cyrillic en look like H, but the lower-case
// eta looks like n, so the capitals cannot ride the lower-case table.
func TestFoldSpokenCapitalLookalikes(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"\u041d": "h", "\u043d": "h", "\u0397": "h", "\u03b7": "n", "\u039d": "n", "\u03bd": "v",
		"\u03a5": "y", "\u039c": "m", "\u041c": "m", "\u0410": "a", "\u0391": "a", "\u0420": "p", "\u03a1": "p",
	}
	for in, want := range tests {
		if got := foldSpoken(in); got != want {
			t.Fatalf("foldSpoken(%U) = %q, want %q", []rune(in)[0], got, want)
		}
	}
}
