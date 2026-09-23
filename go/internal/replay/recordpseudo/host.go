// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo

import (
	"regexp"
	"strings"
)

// semverTagRe is the structural image-tag grammar that is kept: an optional
// v and dotted digits. Anything else in a tag is a customer-chosen word.
var semverTagRe = regexp.MustCompile(`^v?[0-9]+(?:\.[0-9]+)*$`)

// structuralImageTag reports a tag that carries no customer data.
func structuralImageTag(tag string) bool {
	return tag == "" || tag == "latest" || semverTagRe.MatchString(tag)
}

// learnImageTag pseudonymizes a customer-chosen image tag to the name form
// (class-free HMAC, so the tag field and the tag inside an image reference
// agree); structural tags are kept.
func (d *dictionary) learnImageTag(tag string) {
	if structuralImageTag(tag) {
		return
	}
	d.set(ClassIdent, tag, d.name(tag))
}

// awsHostSuffixes are the AWS-owned DNS suffixes whose tail labels are kept
// (count = labels kept from the right, before the region search).
var awsHostSuffixes = []struct {
	suffix string
	tail   int
}{
	{suffix: ".amazonaws.com", tail: 3},
	{suffix: ".on.aws", tail: 2},
	{suffix: ".cloudfront.net", tail: 2},
	{suffix: ".awsapps.com", tail: 2},
}

// awsHostServiceLabels are labels left of the region in an AWS-owned host
// that name the service, not the customer, and are kept verbatim.
var awsHostServiceLabels = map[string]struct{}{
	"dkr": {}, "ecr": {}, "lambda-url": {}, "execute-api": {}, "s3": {}, "s3-website": {},
	"elb": {}, "rds": {}, "es": {}, "cache": {}, "sqs": {}, "sns": {}, "sts": {},
	"awsapprunner": {}, "appsync-api": {}, "elasticbeanstalk": {}, "cloudfront": {},
}

func lastLabelAlphabetic(host string) bool {
	host = strings.TrimSuffix(host, ".")
	label := host[strings.LastIndex(host, ".")+1:]
	if label == "" {
		return false
	}
	for _, r := range label {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return false
		}
	}
	return true
}

// safeHost reports a hostname that already carries no organisation data:
// the reserved names and zones the private-data gate admits unconditionally
// and the public hosts on the gate-mirrored allow list (publicHostsList in
// verify_forms.go, one list shared with Verify), so ghcr.io or
// registry.npmjs.org stay readable instead of collapsing to h....example.
func safeHost(lower string) bool {
	if _, public := publicHostsList[lower]; public {
		return true
	}
	for _, suffix := range []string{".example", ".test", ".invalid", ".localhost", "example.com", "example.net", "example.org"} {
		if lower == strings.TrimPrefix(suffix, ".") || strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return lower == "localhost"
}

// learnHost pseudonymizes a DNS name label by label. Customer labels become
// h+10hex (class-free HMAC of the label), the public suffix collapses to
// "example", wildcard labels and a trailing dot are kept, and AWS-owned
// suffixes keep their structural tail.
func (d *dictionary) learnHost(raw string) {
	host := strings.TrimSuffix(raw, ".")
	trailingDot := strings.HasSuffix(raw, ".")
	lower := strings.ToLower(host)
	if safeHost(lower) {
		return
	}
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		d.set(ClassIdent, raw, d.name(raw))
		return
	}
	var out []string
	if match := ecrHostRe.FindStringSubmatch(lower); match != nil {
		d.learnAccount(match[1])
		out = append([]string{d.account(match[1])}, labels[1:]...)
	} else if tail, aws := awsSuffixTail(lower); aws {
		out = d.awsHostLabels(labels, tail)
	} else {
		out = make([]string, 0, len(labels))
		for i, label := range labels {
			switch {
			case i == len(labels)-1:
				out = append(out, "example")
			case label == "*":
				out = append(out, label)
			default:
				out = append(out, "h"+d.key.hexOf(label, 10))
			}
		}
	}
	pseudonym := strings.Join(out, ".")
	if trailingDot {
		pseudonym += "."
	}
	d.set(ClassHost, raw, pseudonym)
}

func awsSuffixTail(lower string) (int, bool) {
	for _, candidate := range awsHostSuffixes {
		if strings.HasSuffix(lower, candidate.suffix) {
			return candidate.tail, true
		}
	}
	return 0, false
}

// awsHostLabels keeps the suffix tail, the region label and everything right
// of it, and the known service labels; account-shaped labels become account
// pseudonyms and every other customer label becomes h+10hex. The region is
// found by the finite AWS region grammar (awsRegionExactRe), never by a
// loose shape: a customer label such as db-main-1 only looks like a region,
// and treating it as one would keep it and every label right of it raw.
func (d *dictionary) awsHostLabels(labels []string, tail int) []string {
	keepFrom := len(labels) - tail
	for i := 0; i < keepFrom; i++ {
		if awsRegionExactRe.MatchString(strings.ToLower(labels[i])) {
			keepFrom = i
			break
		}
	}
	out := make([]string, 0, len(labels))
	for i, label := range labels {
		lower := strings.ToLower(label)
		switch {
		case i >= keepFrom, label == "*":
			out = append(out, label)
		case account12Re.MatchString(label):
			d.learnAccount(label)
			out = append(out, d.account(label))
		default:
			if _, service := awsHostServiceLabels[lower]; service {
				out = append(out, label)
				continue
			}
			out = append(out, "h"+d.key.hexOf(label, 10))
		}
	}
	return out
}

// learnImageRef splits scheme, host, repository path, tag and digest. The
// host is a ClassHost value, the repository path is learned whole as an
// identifier, the tag per learnImageTag, and the digest is structural and
// kept. A customer tag is also learned as the "path:tag" composite so a
// tag below the free-text length floor is still rewritten inside the
// reference. It never hands a string it received back to learnIdent
// unchanged: learnIdent routes every ".dkr.ecr." string here, so each
// branch below must make progress or stop.
func (d *dictionary) learnImageRef(raw string) {
	ref := raw
	if i := strings.Index(ref, "://"); i >= 0 {
		ref = ref[i+3:]
	}
	ref, _, _ = strings.Cut(ref, "@")
	ref = strings.TrimLeft(ref, "/")
	host, path, hasPath := strings.Cut(ref, "/")
	if !hasPath {
		hostOnly, tag, _ := strings.Cut(ref, ":")
		d.learnImageTag(tag)
		if hostShapeRe.MatchString(hostOnly) && lastLabelAlphabetic(hostOnly) {
			d.learnHost(hostOnly)
		} else if hostOnly != "" {
			d.set(ClassIdent, hostOnly, d.name(hostOnly))
		}
		return
	}
	if strings.ContainsAny(host, ".:") || host == "localhost" {
		hostOnly, _, _ := strings.Cut(host, ":")
		d.learnHost(hostOnly)
	} else if host != "" {
		d.learnIdent(host)
		path = host + "/" + path
	}
	tag := ""
	if i := strings.LastIndex(path, ":"); i >= 0 && !strings.Contains(path[i:], "/") {
		path, tag = path[:i], path[i+1:]
		d.learnImageTag(tag)
	}
	if path == "" {
		return
	}
	if !d.settled(ClassIdent, path) {
		if strings.Contains(path, ".dkr.ecr.") {
			d.set(ClassIdent, path, d.name(path))
		} else {
			d.learnIdent(path)
		}
	}
	if !structuralImageTag(tag) {
		d.set(ClassIdent, path+":"+tag, d.pseudonym(path)+":"+d.pseudonym(tag))
	}
}
