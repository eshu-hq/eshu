// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ocr

import "testing"

// markerLabelForms lists, per marker family, the label shapes an engine reads
// from a form: separator variants, case, and the single-character misreads
// tesseract makes. Every form must be a marker.
var markerLabelForms = map[string][]string{
	"password": {
		"Password", "PASSWORD:", "PassWord", "passwords", "Password1", "DB_PASSWORD", "mypassword",
		"Passw0rd:", "P@ssword", "P@ssw0rd", "Pa55word", "pa$$word", "Pa$$w0rd", "Passw0rd1",
	},
	"passwd and pwd": {"Passwd:", "PASSWD", "PWD:", "pwd", "Pwd=", "PWDs", "pwd2", "PWD1:"},
	"digit tail":     {"Token2:", "secret2", "PIN2", "api key1", "API_KEY2", "access key3", "Secret1:", "TOKEN9="},
	"case misread":   {"ToKen:", "SeCret:", "tOken", "ToKeN2", "PiN:", "PaSsWoRd"},
	"fullwidth": {
		"Ｐａｓｓｗｏｒｄ", "Ｔｏｋｅｎ:", "ＡＰＩ Ｋｅｙ", "ＡＰＩ＿ＫＥＹ", "ＡＰＩ－ＫＥＹ", "Ｐ＠ｓｓｗｏｒｄ", "ｐａ＄＄ｗ０ｒｄ",
	},
	"digit read as a letter after the word": {"Pass cod3", "pass phras3", "PASS_C0D3", "Pass w0rd"},
	"lowercase l is a misread i":            {"Pln:", "pln", "Pln", "PlN"},
	"secret": {
		"Secret:", "SECRET", "secrets", "Client Secret:", "client-secret", "client_secret",
		"clientSecret", "ClientSecret", "Top secret", "S3cret:", "5ecret",
	},
	"token": {
		"Token:", "TOKEN", "tokens", "Access Token:", "access-token", "access_token",
		"accessToken", "GITHUB_TOKEN", "Bearer token", "T0ken:", "AccessToken",
	},
	"api key": {
		"API Key:", "API-Key", "API_KEY", "api key", "APIKey", "apiKey", "apikey:", "Api  Key",
		"API KEYS", "x-api-key", "MYAPI_KEY", "ap1 key", "MYAPI KEY", "myapi-key",
	},
	"access key":  {"Access Key:", "accesskey", "ACCESSKEY:", "AccessKey1", "MYACCESS KEY", "AccessKEYs", "access-key", "Access Key ID", "AWS_ACCESS_KEY_ID", "accessKey"},
	"private key": {"Private Key:", "PrivateKEYs", "myprivate key", "privatekey", "Privatekey", "private-key", "PRIVATE_KEY", "privateKey", "PrivateKey"},
	"credential":  {"Credential:", "Credentials", "credentials.json", "Cr3dentials", "Credentia1s"},
	"passphrase":  {"Passphrase:", "Pass phrase", "pass-phrase", "PassPhrase"},
	"passcode":    {"Passcode:", "Pass code", "PassCode", "PASS_CODE"},
	"pin":         {"PIN:", "Pin", "pin code", "Enter PIN", "p1n", "PINs", "$PIN", "(PIN)", "Pincode:", "PINCODE", "pin_code"},
	"legacy": {
		"credential_marker visible label", "secret_marker", "token_marker", "private_key", "api_key",
	},
}

// ordinaryLines are text that must not be a marker. Each one holds a marker
// word as a substring or one edit away, so a loose boundary rule turns it red.
var ordinaryLines = []string{
	"Deploy the Checkout Service to staging", "Rollback steps", "Release checklist",
	"spinning wheel", "Pinterest board", "pinpoint the cause", "pinned version", "tokenize the input",
	"tokenizer settings", "tokenization", "Secretary of state", "secretly", "secretion", "passage",
	"bypass the cache", "compass", "passport", "passenger", "keyboard", "monkey", "key lime pie",
	"public key", "primary key", "foreign key", "access control", "API gateway", "private network",
	"private", "access", "key", "api", "bypass code", "compass code", "Pass the code review",
	"Call the API, key facts follow", "Access: key facts", "broken link", "taken offline", "v5.0.1",
	"Level 5 of 10", "pass through", "500 PLN", "PLN", "Price 20 PLN", "spin2", "tokenize2", "secretary2",
	"keyboard1", "Pinterest2", "pinned1", "the shop is open", "Hello world", "Cost is $5 per seat",
}

func TestContainsMarkerWordMatchesEveryLabelForm(t *testing.T) {
	t.Parallel()

	for family, forms := range markerLabelForms {
		for _, form := range forms {
			form := form
			t.Run(family+"/"+form, func(t *testing.T) {
				t.Parallel()
				if !containsMarkerWord(form) {
					t.Fatalf("containsMarkerWord(%q) = false, want true", form)
				}
			})
		}
	}
}

func TestContainsMarkerWordLeavesOrdinaryTextAlone(t *testing.T) {
	t.Parallel()

	for _, line := range ordinaryLines {
		line := line
		t.Run(line, func(t *testing.T) {
			t.Parallel()
			if containsMarkerWord(line) {
				t.Fatalf("containsMarkerWord(%q) = true, want false", line)
			}
		})
	}
}

// TestContainsMarkerWordBoundaries pins the shape of the rule, not one word
// list: the same word is a marker as a whole word or compound suffix, and an
// ordinary word that merely contains it is not.
func TestContainsMarkerWordBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		text string
		want bool
	}{
		{"pin", true},
		{"spin", false},
		{"pins", true},
		{"pining", false},
		{"token", true},
		{"tokens", true},
		{"authtoken", true},
		{"tokenize", false},
		{"secrets", true},
		{"topsecret", true},
		{"secretary", false},
		{"api key", true},
		{"api-key", true},
		{"api\tkey", true},
		{"api . key", false},
		{"api, key", false},
		{"api keyboard", false},
		{"apikeys", true},
		{"a pi key", false},
		{"pass phrase", true},
		{"pass the phrase", false},
		{"|Token|", true},
		{"|Pin|", true},
		{"$ecret", false},
		{"@token", true},
		{"p@sscode", true},
		{"", false},
		{"   ", false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.text, func(t *testing.T) {
			t.Parallel()
			if got := containsMarkerWord(tt.text); got != tt.want {
				t.Fatalf("containsMarkerWord(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

// TestSourceIdentityKeepsTheNarrowMarkerList pins that the wider OCR label list
// does not reach source names. A file called token-rotation.png is a name, not
// text read from a picture, and redacting it would hide a document's identity
// for no gain.
func TestSourceIdentityKeepsTheNarrowMarkerList(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"docs/token-rotation.png", "docs/pin-the-version.png", "docs/api-key-flow.png", "docs/secret-santa.png"} {
		if got := safeSourceURI(name); got != name {
			t.Fatalf("safeSourceURI(%q) = %q, want it unchanged", name, got)
		}
		if got, redacted := safeIdentity(name, "ocr"); redacted || got != name {
			t.Fatalf("safeIdentity(%q) = %q, %v, want it unchanged", name, got, redacted)
		}
	}
	if got := safeSourceURI("docs/password-reset.png"); got == "docs/password-reset.png" {
		t.Fatalf("safeSourceURI kept a password name, want it redacted")
	}
}
