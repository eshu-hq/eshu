// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo

import (
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// rfc5737Blocks are the IPv4 documentation ranges; hosts .1-.254 of each give
// 762 slots.
var rfc5737Blocks = [3]string{"192.0.2.", "198.51.100.", "203.0.113."}

const ipv4Slots = 3 * 254

func (d *dictionary) learnIPv4(raw string) {
	ip := net.ParseIP(raw)
	if ip == nil || ip.To4() == nil {
		d.learnIdent(raw)
		return
	}
	if keepIPv4(raw, ip) {
		return
	}
	d.set(ClassIPv4, raw, d.ipv4Slot(raw))
}

func keepIPv4(raw string, ip net.IP) bool {
	if ip.IsLoopback() || ip.IsUnspecified() || raw == "255.255.255.255" {
		return true
	}
	for _, block := range rfc5737Blocks {
		if strings.HasPrefix(raw, block) {
			return true
		}
	}
	return false
}

// ipv4Slot maps the address into RFC 5737 by HMAC, linear-probing past slots
// already owned by a different raw address so two raw addresses never share
// a pseudonym within one recording. Every probe step is counted.
func (d *dictionary) ipv4Slot(raw string) string {
	idx := int(binary.BigEndian.Uint64(d.key.mac(raw)) % ipv4Slots)
	for tries := 0; tries < ipv4Slots; tries++ {
		owner, used := d.ipSlots[idx]
		if !used || owner == raw {
			d.ipSlots[idx] = raw
			return rfc5737Blocks[idx/254] + strconv.Itoa(idx%254+1)
		}
		d.ipCollisions++
		idx = (idx + 1) % ipv4Slots
	}
	panic("recordpseudo: RFC 5737 slot space exhausted")
}

func (d *dictionary) learnIPv6(raw string) {
	ip := net.ParseIP(raw)
	if ip == nil || ip.To4() != nil {
		d.learnIdent(raw)
		return
	}
	if ip.IsLoopback() || ip.IsUnspecified() || strings.HasPrefix(strings.ToLower(raw), "2001:db8:") {
		return
	}
	d.set(ClassIPv6, raw, d.ipv6Pseudonym(raw))
}

// ipv6Pseudonym puts 96 bits of HMAC behind the 2001:db8::/32 prefix.
func (d *dictionary) ipv6Pseudonym(raw string) string {
	mac := d.key.mac(raw)
	groups := make([]string, 0, 8)
	groups = append(groups, "2001", "db8")
	for i := 0; i < 6; i++ {
		groups = append(groups, fmt.Sprintf("%x", binary.BigEndian.Uint16(mac[i*2:i*2+2])))
	}
	return strings.Join(groups, ":")
}

func (d *dictionary) learnCIDR(raw string) {
	network, prefix, ok := strings.Cut(raw, "/")
	if !ok {
		d.learnIdent(raw)
		return
	}
	ip := net.ParseIP(network)
	switch {
	case ip == nil:
		d.set(ClassIdent, raw, d.name(raw))
	case ip.IsUnspecified() || ip.IsLoopback():
		return
	case ip.To4() != nil:
		if keepIPv4(network, ip) {
			return
		}
		d.set(ClassCIDR, raw, d.ipv4Slot(network)+"/"+prefix)
	default:
		if strings.HasPrefix(strings.ToLower(network), "2001:db8:") {
			return
		}
		d.set(ClassCIDR, raw, d.ipv6Pseudonym(network)+"/"+prefix)
	}
}

func (d *dictionary) learnEmail(raw string) {
	d.set(ClassEmail, raw, d.key.hexOf(raw, 11)+"@example.com")
}

// isIPv6 reports a parseable IPv6 literal (never an IPv4 one).
func isIPv6(raw string) bool {
	ip := net.ParseIP(raw)
	return ip != nil && ip.To4() == nil
}
