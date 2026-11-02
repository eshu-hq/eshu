// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package media

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// phraseRule is one folded keyword and where it must sit in a word. A phrase
// with neither flag matches as a substring, the way the original keyword list
// did, so "password" still finds "passwords" and "mypassword". Over-redaction
// there is accepted policy. before and after require a non-letter (or the text
// edge) on that side of the phrase, which keeps short or ambiguous words from
// firing inside longer ones.
type phraseRule struct {
	phrase string
	before bool
	after  bool
}

// spokenKeywordRules are matched against the folded text of a segment, where
// every separator run is one space and every letter is lower case.
//
// The boundary choice for each rule is deliberate:
//
//   - Substring (no flags): password, passphrase, passcode, passwd, pwd,
//     pincode, credential, api key, apikey, private key, privatekey, and the
//     *_marker test keywords. None is a fragment of an ordinary English word, and a hit
//     inside a longer word ("credentials", "passwords") is the same secret
//     vocabulary.
//   - Start-bounded: the split spellings "pass word", "pass phrase", and
//     "pass code", the spelled-out "a p i key", and "accesskey". Unbounded they
//     would fire inside "compass word" or "bypass wording". The joined
//     spellings of pass word, pass phrase, and pass code need no bound.
//   - Whole word, access key: "access key" and "access keys" are bounded on both
//     sides so "access keyboard" stays clear.
//   - Whole word: secret, secrets, token, tokens, pin, and the spelled-out
//     p i n and p w d. A substring "token" would redact "tokenize" and
//     "tokenizer", and "pin" would redact "spinning", "pinned", "mapping", and
//     "shipping". The cost is that "tokenized" and "pins" stay in clear text,
//     which is the intended trade: whisper writes a spoken secret label as its
//     own word. "secret" fails closed on purpose: "a secret garden" redacts
//     two following segments, which is visible in section metadata, while a
//     persisted client secret is not recoverable.
var spokenKeywordRules = []phraseRule{
	{phrase: "password"},
	{phrase: "passphrase"},
	{phrase: "passcode"},
	{phrase: "passwd"},
	{phrase: "pwd"},
	{phrase: "pincode"},
	{phrase: "credential"},
	{phrase: "api key"},
	{phrase: "apikey"},
	{phrase: "private key"},
	{phrase: "privatekey"},
	{phrase: "secret marker"},
	{phrase: "token marker"},
	{phrase: "pass word", before: true},
	{phrase: "pass phrase", before: true},
	{phrase: "pass code", before: true},
	{phrase: "a p i key", before: true},
	{phrase: "accesskey", before: true},
	{phrase: "access key", before: true, after: true},
	{phrase: "access keys", before: true, after: true},
	{phrase: "secret", before: true, after: true},
	{phrase: "secrets", before: true, after: true},
	{phrase: "token", before: true, after: true},
	{phrase: "tokens", before: true, after: true},
	{phrase: "pin", before: true, after: true},
	{phrase: "p i n", before: true, after: true},
	{phrase: "p w d", before: true, after: true},
}

// homoglyphs folds the lower-case Cyrillic, Greek, and Latin look-alikes of
// the Latin letters that the keywords above use (a c d e h i k l m n o p r s t
// v w y). It is a bounded set, not the full Unicode confusables table: a
// look-alike for a letter no keyword uses is not worth a rule, and a letter
// outside this set is a documented limit. foldSpoken lower-cases a rune before
// it looks here, so a capital look-alike folds through its lower-case form,
// except the ones in capitalHomoglyphs.
var homoglyphs = map[rune]rune{
	// Cyrillic.
	'\u0430': 'a', '\u0435': 'e', '\u043e': 'o', '\u0440': 'p', '\u0441': 'c',
	'\u0443': 'y', '\u0456': 'i', '\u0455': 's', '\u04bb': 'h', '\u0501': 'd',
	'\u051d': 'w', '\u043a': 'k', '\u0475': 'v', '\u0442': 't', '\u043c': 'm',
	'\u043f': 'n', '\u04cf': 'l', '\u043d': 'h',
	// Greek.
	'\u03bf': 'o', '\u03c1': 'p', '\u03bd': 'v', '\u03b1': 'a', '\u03b5': 'e',
	'\u03b9': 'i', '\u03ba': 'k', '\u03c4': 't', '\u03b7': 'n',
	// Latin.
	'\u0131': 'i',
}

// capitalHomoglyphs holds the capitals whose lower-case form looks like a
// different letter than the capital does: Greek capital nu looks like N but
// small nu looks like v; capital upsilon looks like Y; capital eta looks like H
// but small eta looks like n; capital mu looks like M. Cyrillic capital en
// folds through its small form, which looks like H too.
var capitalHomoglyphs = map[rune]rune{
	'\u039d': 'n', '\u03a5': 'y', '\u0397': 'h', '\u039c': 'm',
}

// containsSpokenKeyword reports whether the folded form of text holds a spoken
// secret keyword. Folding turns "_" into a space, so the rules also cover every
// legacy keyword ("api_key" folds to "api key"); sanitize_keyword_test.go pins
// that.
func containsSpokenKeyword(text string) bool {
	return foldedHasKeyword(foldSpoken(text))
}

// foldedHasKeyword reports whether already-folded text matches a keyword rule.
func foldedHasKeyword(folded string) bool {
	for _, rule := range spokenKeywordRules {
		if containsPhrase(folded, rule) {
			return true
		}
	}
	return false
}

// joinEdgeBytes is how many bytes of each segment joinedHasKeyword looks at.
// The longest rule phrase is 13 bytes, so a keyword that straddles the join
// starts in the last 12 bytes of the first segment and ends in the first 12 of
// the second. 24 leaves room for both.
const joinEdgeBytes = 24

// edgeLetter stands in for the letter a byte cut removed. No rule phrase holds
// an "x", so it never takes part in a match; it only makes a before or after
// boundary check see a letter where the cut split a word, instead of the text
// edge that would pass the check.
const edgeLetter = "x"

// joinedHasKeyword reports whether a keyword straddles two consecutive
// segments, such as "The API" then "key is". Both arguments are folded. A
// caller passes only a pair where neither segment holds a keyword alone, so a
// hit is a keyword that needed both halves. The join is one space, which is
// what the fold makes of any separator, so "pass" then "word" reads as "pass
// word". Only the last joinEdgeBytes of the first segment and the first
// joinEdgeBytes of the second are joined, which keeps the work constant per
// pair. A cut inside a word leaves an edgeLetter in its place, so a long word
// next to the join is kept and a cut cannot fake a word boundary.
func joinedHasKeyword(prevFolded, curFolded string) bool {
	if prevFolded == "" || curFolded == "" {
		return false
	}
	return foldedHasKeyword(edgeTail(prevFolded, joinEdgeBytes) + " " + edgeHead(curFolded, joinEdgeBytes))
}

// edgeTail returns the end of folded, at most n bytes, with an edgeLetter in
// front when the cut fell inside a word.
func edgeTail(folded string, n int) string {
	if len(folded) <= n {
		return folded
	}
	if folded[len(folded)-n-1] == ' ' {
		return folded[len(folded)-n:]
	}
	return edgeLetter + folded[len(folded)-n:]
}

// edgeHead returns the start of folded, at most n bytes, with an edgeLetter
// after it when the cut fell inside a word.
func edgeHead(folded string, n int) string {
	if len(folded) <= n {
		return folded
	}
	if folded[n] == ' ' {
		return folded[:n]
	}
	return folded[:n] + edgeLetter
}

// foldSpoken returns text in the form the spoken keyword rules match on. It
// is deterministic and linear in the length of text. It
//
//   - decomposes compatibility forms (fullwidth letters, ligatures, no-break
//     and ideographic spaces) and drops the combining marks that remain, so
//     "pássword" folds to "password";
//   - drops format characters (soft hyphen, zero-width space and joiners, word
//     joiner, byte order mark, bidirectional marks), which are invisible in a
//     transcript but split a word for a byte-wise match;
//   - lower-cases and maps the homoglyph set above to Latin;
//   - turns every run of white space, dash punctuation, underscore, dot, and
//     comma into one space, so "pass-word", "pass_word", and "pass.word" fold
//     like "pass word", "P.I.N." and "P, I, N" fold to "p i n", and "A.P.I.
//     key" folds to "a p i key".
func foldSpoken(text string) string {
	if isASCII(text) {
		return foldASCII(text)
	}
	return foldUnicode(text)
}

// isASCII reports whether every byte of text is below 0x80.
func isASCII(text string) bool {
	for i := 0; i < len(text); i++ {
		if text[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// foldASCII is foldUnicode for text that is all ASCII, without the
// normalization and table lookups. ASCII holds no combining mark, format
// character, or homoglyph, so only case and separators remain; a test pins
// that it agrees with foldUnicode on every ASCII byte.
func foldASCII(text string) string {
	out := make([]byte, 0, len(text))
	pendingSpace := false
	for i := 0; i < len(text); i++ {
		b := text[i]
		switch {
		case b == ' ' || (b >= '\t' && b <= '\r') || b == '_' || b == '.' || b == ',' || b == '-':
			pendingSpace = len(out) > 0
			continue
		case b >= 'A' && b <= 'Z':
			b += 'a' - 'A'
		}
		if pendingSpace {
			out = append(out, ' ')
			pendingSpace = false
		}
		out = append(out, b)
	}
	return string(out)
}

// foldUnicode is the general fold: see foldSpoken.
func foldUnicode(text string) string {
	var out strings.Builder
	out.Grow(len(text))
	pendingSpace := false
	for _, r := range norm.NFKD.String(text) {
		switch {
		case unicode.Is(unicode.Mn, r), unicode.Is(unicode.Cf, r):
			continue
		case unicode.IsSpace(r), r == '_', r == '.', r == ',', unicode.Is(unicode.Pd, r):
			pendingSpace = out.Len() > 0
			continue
		}
		if latin, ok := capitalHomoglyphs[r]; ok {
			r = latin
		} else {
			r = unicode.ToLower(r)
			if latin, ok := homoglyphs[r]; ok {
				r = latin
			}
		}
		if pendingSpace {
			out.WriteByte(' ')
			pendingSpace = false
		}
		out.WriteRune(r)
	}
	return out.String()
}

// containsPhrase reports whether folded holds rule.phrase with the boundaries
// the rule asks for. Every occurrence is tried, so a rejected one does not hide
// a later accepted one.
func containsPhrase(folded string, rule phraseRule) bool {
	for offset := 0; offset <= len(folded)-len(rule.phrase); {
		index := strings.Index(folded[offset:], rule.phrase)
		if index < 0 {
			return false
		}
		start := offset + index
		end := start + len(rule.phrase)
		if (!rule.before || !letterBefore(folded, start)) && (!rule.after || !letterAt(folded, end)) {
			return true
		}
		offset = start + 1
	}
	return false
}

func letterBefore(s string, index int) bool {
	r, size := utf8.DecodeLastRuneInString(s[:index])
	return size > 0 && unicode.IsLetter(r)
}

func letterAt(s string, index int) bool {
	r, size := utf8.DecodeRuneInString(s[index:])
	return size > 0 && unicode.IsLetter(r)
}
