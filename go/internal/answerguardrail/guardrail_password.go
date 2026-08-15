// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package answerguardrail

import (
	"regexp"
	"strings"
)

// valueCharClass is the set of characters a password assignment's value may
// carry and still be classifiable as a type name, a count, or a placeholder:
// lowercase letters, digits, and the punctuation those spellings use --
// "[]byte", "*string", "my_type", "pkg.Type", "kebab-case".
//
// It is a regex character-class body, and it is the ONLY copy of this set.
// passwordAssignmentPattern interpolates it, so the capture stops exactly where
// the classifier stops caring. An earlier version captured everything up to
// whitespace, a quote, or a comma and trimmed the result down to this same set
// in a separate Go function; the set then existed twice, in two spellings, with
// nothing keeping them in step.
const valueCharClass = `a-z0-9_.\-*&\[\]`

// passwordAssignmentPattern covers the YAML, log-line, and JSON spelling of a
// password assignment. The fragment list in UnsafeString spells "password=" and
// stops there, so "password: hunter2" -- the form a config dump and a log line
// both use -- published clean.
//
// The key half is a boundary rather than a "\b", and the difference is the
// whole point: "\b" treats "_" as a word character, so "\bpassword" does not
// match after one and DB_PASSWORD:, POSTGRES_PASSWORD:, and my_password: all
// walked past a rule written to catch exactly that. The boundary here excludes
// only letters and digits, so an underscore-joined key matches and
// "checkPassword:" -- where a letter precedes -- does not. Quotes on either
// side of the key are allowed, including backslash-escaped ones, which is what
// covers 'password':, "password":, and \"password\":.
//
// Only "password" gets a colon rule; "token", "secret", and "api_key" keep
// their "=" form only, because real resource types end in those words
// ("aws_appsync_api_key: 2 resources", "aws_secretsmanager_secret: 5
// resources") and a colon rule on them would reject honest answers. That is the
// same false positive evidencebundle's credentialPattern comment records
// hitting on "secrets_iam_trust_chain".
//
// The trailing group captures the assigned value, because the keyword alone
// does not say a credential is being assigned -- see passwordAssignmentIsUnsafe,
// which classifies it. The capture is valueCharClass, so it ends at the first
// character a type name or a count cannot hold. That is what lets one pass find
// every assignment in the string: the separator between two run-together
// assignments is by definition a character the capture stops at, so it is still
// there to serve as the next match's left boundary.
//
// Matched against the already-lowercased copy, so it carries no (?i).
var passwordAssignmentPattern = regexp.MustCompile(
	`(^|[^a-z0-9])password[\\"']*[ \t]*:[ \t]*[\\"']*([` + valueCharClass + `]*)`,
)

// passwordDeclarationValues are the assigned values that make a "password:" line
// a type declaration rather than a credential. A schema, an interface, or a
// GraphQL field says what shape the password has and carries no password:
// TypeScript "password: string", GraphQL "password: String!", Rust
// "password: Option<String>", Python "password: SecretStr". Withholding an
// answer about a schema is the false positive this list exists to prevent.
//
// Matched after the value is lowercased and stripped of pointer, reference, and
// slice punctuation. The capture class already ended the value at the first
// character a type name cannot contain -- "<", "(", "!", "|" -- so one entry
// covers "String!", "Option<String>", and "varchar(255)" alike.
//
// A type name outside this list is treated as a value and the answer is
// withheld. The list covers the scalars the colon-declaration languages
// actually spell; a project-specific type is the accepted miss.
var passwordDeclarationValues = map[string]bool{
	"any": true, "bool": true, "boolean": true, "buffer": true,
	"byte": true, "bytes": true, "char": true, "float": true,
	"int": true, "integer": true, "nil": true, "none": true,
	"null": true, "number": true, "object": true, "option": true,
	"optional": true, "secretstr": true, "str": true, "string": true,
	"text": true, "undefined": true, "unknown": true, "uuid": true,
	"varchar": true,
}

// passwordAssignmentIsUnsafe reports whether lower carries a password key that
// is actually being assigned a credential. The keyword is not the
// discriminator, the value is -- the same conclusion evidencebundle's
// credentialPattern reached, and for the same reason: real content puts a count
// after a name ending in a credential word. Terraform's random_password is one,
// so "random_password: 3 resources" has to stay publishable, exactly as
// "aws_secretsmanager_secret: 5 resources" already does.
//
// Three value shapes read as something other than a credential:
//
//   - A declaration. "password: string" assigns a TYPE, so the answer is about
//     a schema. See passwordDeclarationValues.
//   - A count. "random_password: 3 resources" assigns a number.
//   - A placeholder. "password: <redacted>", "password: ***", and
//     "password: ${DB_PASSWORD}" start with punctuation and name no secret.
//
// Anything else -- "password: hunter2", "DB_PASSWORD: hunter2",
// "'password': 'S3NT1NEL'" -- is a credential and is withheld.
//
// Two accepted misses, both the price of a false positive worth more:
//
//   - A key that runs the word together with its prefix (PGPASSWORD:) is
//     shape-identical to checkPassword: and is not screened.
//   - A digits-only or punctuation-only password ("password: 123456",
//     "password: !!!") reads as a count or a placeholder. evidencebundle's
//     credentialPattern documents the same gap.
//
// Every assignment the key rule can see is classified, not only the leftmost
// one. The keyword is not the discriminator and the value is, so one honest
// assignment says nothing about the next: "password: string, password: hunter2"
// is a declaration followed by a credential, and a screen that returned on the
// first match published the second. That holds when the two run together with
// no space, quote, or comma between them -- "password:string;password:hunter2"
// -- because the capture stops at the ";".
//
// What this guarantees: for two assignments the key rule matches at all, a
// credential is withheld wherever it sits, and swapping the two does not change
// the answer. What it cannot do is rescue a key the rule never matches --
// "PGPASSWORD:" is unscreened in any position, for the reason above.
//
// One FindAllStringSubmatch pass is what keeps this linear, and the input is
// not capped: ask_sse.go screens the whole joined model answer as one string,
// and answerquality screens evidence values taken from indexed repository
// content. An earlier version captured up to whitespace, a quote, or a comma
// and restarted the regex at each value it classified, which re-scanned the
// remaining string once per assignment. On back-to-back assignments carrying
// none of those three characters, measured on
// BenchmarkUnsafeStringPasswordRunTogetherScale with the two scans interleaved
// and reported as the minimum of 7 samples (3 at 128KB) on a host at load
// average 7-16:
//
//	bytes    restart-per-value    one pass    ratio
//	  4096              3.34ms      94.6us      35x
//	 32768             713.0ms      1.19ms     599x
//	131072              12.31s      4.84ms    2544x
//
// Throughput holds at about 27 MB/s from 32KB up, so the cost now tracks the
// input length rather than the square of the assignment count.
//
// lower must already be lowercased.
func passwordAssignmentIsUnsafe(lower string) bool {
	for _, match := range passwordAssignmentPattern.FindAllStringSubmatch(lower, -1) {
		if passwordValueIsCredential(match[2]) {
			return true
		}
	}
	return false
}

// passwordValueIsCredential reports whether value, one "password:" assignment's
// captured value, reads as a credential rather than as a type name, a count, or
// a placeholder. Split out from passwordAssignmentIsUnsafe so the
// classification is one expression the scan loop cannot accidentally reorder.
func passwordValueIsCredential(value string) bool {
	switch {
	case !hasLetterOrDigit(value):
		// No value, or one made only of punctuation: a placeholder or a mask.
		return false
	case isDigits(value):
		// A count, as in "random_password: 3 resources".
		return false
	case passwordDeclarationValues[strings.Trim(value, "*&[]")]:
		// A type: the answer is about a schema.
		return false
	}
	return true
}

// hasLetterOrDigit reports whether value carries anything a credential could be
// spelled with. Matched against a lowercased value.
func hasLetterOrDigit(value string) bool {
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return true
		}
	}
	return false
}

// isDigits reports whether value is a non-empty run of decimal digits, which is
// a count rather than a credential.
func isDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
