// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo

import (
	"encoding/binary"
	"fmt"
	"regexp"
	"strings"
)

var (
	awsIDRe     = regexp.MustCompile(`^(i|ami|sg|subnet|vpc|eni|vol|igw|rtb|acl|eipalloc|nat|vpce|snap|lt|asg|pl|tgw|vgw|cgw|vpn|fs|fsap|lb|tg)-[0-9a-f]{8,17}$`)
	hex32Re     = regexp.MustCompile(`^[0-9a-f]{32}$`)
	account12Re = regexp.MustCompile(`^[0-9]{12}$`)
	ipv4Re      = regexp.MustCompile(`^(?:[0-9]{1,3}\.){3}[0-9]{1,3}$`)
	cidrRe      = regexp.MustCompile(`^[0-9A-Fa-f.:]+/[0-9]{1,3}$`)
	ecrHostRe   = regexp.MustCompile(`^([0-9]{12})\.dkr\.ecr\.([a-z0-9-]+)\.amazonaws\.com$`)
	numericRe   = regexp.MustCompile(`^[0-9]+$`)
	regionRe    = regexp.MustCompile(`^(?:[a-z]{2}(?:-gov|-iso[a-z]?)?-[a-z]+-[0-9])$`)
	hostShapeRe = regexp.MustCompile(`^\*?[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?)+\.?$`)
	emailRe     = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
)

// structuralWords are tokens that are never learned as identifiers: they are
// enum-like words that also appear in Keep fields (environment names, ELBv2
// load-balancer types, version qualifiers), so pseudonymizing one of them as
// a name would rewrite structural values elsewhere in the same recording.
var structuralWords = map[string]struct{}{
	"prod": {}, "production": {}, "stage": {}, "staging": {}, "dev": {}, "development": {},
	"test": {}, "qa": {}, "latest": {}, "default": {}, "main": {}, "master": {},
	"app": {}, "net": {}, "gwy": {}, "true": {}, "false": {}, "none": {}, "null": {},
	"$LATEST": {}, "aws": {}, "*": {},
}

// dictionary learns raw tokens and their pseudonyms. It is single-goroutine:
// the source wrapper drives it.
type dictionary struct {
	key          Key
	entries      map[string]string
	ipSlots      map[int]string
	ipCollisions int
	learned      map[Class]int
	sorted       []string
}

func newDictionary(key Key) *dictionary {
	return &dictionary{
		key:     key,
		entries: map[string]string{},
		ipSlots: map[int]string{},
		learned: map[Class]int{},
	}
}

func (d *dictionary) set(class Class, raw, pseudonym string) {
	if raw == pseudonym {
		return
	}
	d.entries[raw] = pseudonym
	d.learned[class]++
	d.sorted = nil
}

func (d *dictionary) known(raw string) bool {
	_, ok := d.entries[raw]
	return ok
}

// learn classifies one raw value and records its pseudonym. Empty values,
// wildcards and already-learned tokens are ignored.
func (d *dictionary) learn(class Class, raw string) {
	raw = strings.TrimSpace(raw)
	if raw == "" || d.known(raw) {
		return
	}
	if _, structural := structuralWords[raw]; structural {
		return
	}
	switch class {
	case ClassAccount:
		d.learnAccount(raw)
	case ClassARN:
		d.learnARN(raw)
	case ClassIdent:
		d.learnIdent(raw)
	case ClassAWSID:
		d.learnAWSID(raw)
	case ClassHost:
		d.learnHost(raw)
	case ClassECRRef:
		d.learnImageRef(raw)
	case ClassIPv4:
		d.learnIPv4(raw)
	case ClassIPv6:
		d.learnIPv6(raw)
	case ClassCIDR:
		d.learnCIDR(raw)
	case ClassTagValue:
		d.set(ClassTagValue, raw, "t"+d.key.hexOf(raw, 11))
	case ClassEmail:
		d.learnEmail(raw)
	default:
		// Keep, Opaque and Unknown learn nothing: Keep values are structural,
		// the other two are replaced wholesale at rewrite time.
	}
}

func (d *dictionary) account(raw string) string {
	return fmt.Sprintf("0000%08d", binary.BigEndian.Uint64(d.key.mac(raw))%100000000)
}

func (d *dictionary) name(raw string) string { return "n" + d.key.hexOf(raw, 11) }

func (d *dictionary) learnAccount(raw string) {
	if account12Re.MatchString(raw) {
		d.set(ClassAccount, raw, d.account(raw))
		return
	}
	d.learnIdent(raw)
}

// learnARN keeps partition, service, region, the leading resource-type token
// (a lowercase word such as instance, role, function, task-definition) and
// the ELBv2 type token after loadbalancer/targetgroup; the account and every
// other component are learned. AWS-managed policies (account "aws") are
// public and stay whole.
func (d *dictionary) learnARN(raw string) {
	parts := strings.SplitN(raw, ":", 6)
	if len(parts) < 6 {
		d.learnIdent(raw)
		return
	}
	account := parts[4]
	if account == "aws" {
		return
	}
	if account != "" {
		d.learnAccount(account)
	}
	resource := parts[5]
	if parts[2] == "s3" {
		d.learnIdent(strings.SplitN(resource, "/", 2)[0])
		return
	}
	components := strings.FieldsFunc(resource, func(r rune) bool { return r == '/' || r == ':' })
	skip := 0
	if len(components) >= 2 && isTypeToken(components[0]) {
		skip = 1
		if len(components) >= 3 && (components[0] == "loadbalancer" || components[0] == "targetgroup") {
			if _, ok := map[string]struct{}{"app": {}, "net": {}, "gwy": {}}[components[1]]; ok {
				skip = 2
			}
		}
	}
	for _, component := range components[skip:] {
		d.learnIdent(component)
	}
}

func isTypeToken(component string) bool {
	if component == "" {
		return false
	}
	for _, r := range component {
		if (r < 'a' || r > 'z') && r != '-' {
			return false
		}
	}
	return true
}

// learnIdent sniffs the shape of a name-like value and delegates.
func (d *dictionary) learnIdent(raw string) {
	raw = strings.TrimSpace(raw)
	if raw == "" || d.known(raw) {
		return
	}
	if _, structural := structuralWords[raw]; structural {
		return
	}
	switch {
	case strings.HasPrefix(raw, "arn:"):
		d.learnARN(raw)
	case strings.HasPrefix(raw, "workload:"):
		d.learnIdent(strings.TrimPrefix(raw, "workload:"))
	case strings.HasPrefix(raw, "service:") && !strings.Contains(raw, "/"):
		d.learnIdent(strings.TrimPrefix(raw, "service:"))
	case strings.Contains(raw, ".dkr.ecr."):
		d.learnImageRef(raw)
	case strings.HasPrefix(raw, "sha256:"):
		return
	case strings.Contains(raw, "@sha256:"):
		before, _, _ := strings.Cut(raw, "@")
		d.learnIdent(before)
	case cidrRe.MatchString(raw):
		d.learnCIDR(raw)
	case strings.Contains(raw, ":") && !strings.Contains(raw, "/"):
		// name:revision or repo:tag: the suffix is structural.
		before, _, _ := strings.Cut(raw, ":")
		d.learnIdent(before)
	case awsIDRe.MatchString(raw):
		d.set(ClassAWSID, raw, d.awsID(raw))
	case hex32Re.MatchString(raw):
		d.set(ClassAWSID, raw, d.key.hexOf(raw, 32))
	case account12Re.MatchString(raw):
		d.set(ClassAccount, raw, d.account(raw))
	case ipv4Re.MatchString(raw):
		d.learnIPv4(raw)
	case numericRe.MatchString(raw):
		return
	case emailRe.MatchString(raw):
		d.learnEmail(raw)
	case hostShapeRe.MatchString(raw) && lastLabelAlphabetic(raw):
		d.learnHost(raw)
	default:
		d.set(ClassIdent, raw, d.name(raw))
	}
}

func (d *dictionary) awsID(raw string) string {
	i := strings.Index(raw, "-")
	return raw[:i+1] + d.key.hexOf(raw, len(raw)-i-1)
}

func (d *dictionary) learnAWSID(raw string) {
	switch {
	case awsIDRe.MatchString(raw):
		d.set(ClassAWSID, raw, d.awsID(raw))
	case hex32Re.MatchString(raw):
		d.set(ClassAWSID, raw, d.key.hexOf(raw, 32))
	default:
		d.learnIdent(raw)
	}
}
