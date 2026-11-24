// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"log/slog"
	"net"

	"github.com/jackc/pgx/v5"
)

// tryRequalifyMember refreshes one fleet member's frozen identity after a
// member-local topology mismatch (a standby restart changes its postmaster
// incarnation and possibly its addresses). The fresh identity is published
// only after the member proves it still serves the lined-up cluster, so a
// replaced-by-foreign member stays excluded. It returns false when the member
// must stay excluded, in which case the caller keeps the legacy behavior.
func (a *Access) tryRequalifyMember(ctx context.Context, member int) bool {
	if a == nil || member < 0 || member >= len(a.readerMembers) {
		return false
	}
	if err := ctx.Err(); err != nil {
		return false
	}
	a.requalifyMu.Lock()
	defer a.requalifyMu.Unlock()
	m := &a.readerMembers[member]
	read := a.readMemberIdentity
	if read == nil {
		if m.dial == nil {
			return false
		}
		read = a.dialMemberIdentity
	}
	id, serverAddress, addresses, err := read(ctx, m)
	if err != nil {
		return false
	}
	return a.publishRequalifiedIdentity(member, id, serverAddress, addresses)
}

// dialMemberIdentity reads one member's physical identity over a throwaway
// connection. It mirrors the bootstrap qualification reads without building a
// pool, so requalification never borrows from the budgets it is trying to
// repair.
func (a *Access) dialMemberIdentity(ctx context.Context, m *physicalReaderMember) (physicalIdentity, string, []net.IP, error) {
	cfg := m.dial.Copy()
	cfg.ValidateConnect = nil
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, m.host)
	if err != nil || len(addresses) == 0 {
		if err == nil {
			err = errors.New("no member addresses")
		}
		return physicalIdentity{}, "", nil, err
	}
	physicalAddresses := make([]net.IP, 0, len(addresses))
	for _, address := range addresses {
		physicalAddresses = append(physicalAddresses, address.IP)
	}
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return physicalIdentity{}, "", nil, err
	}
	readErr := a.attestReaderMember(ctx, conn)
	var id physicalIdentity
	var serverAddress string
	if readErr == nil {
		id, readErr = readPhysicalPGX(ctx, conn)
	}
	if readErr == nil {
		readErr = conn.QueryRow(ctx, "SELECT host(inet_server_addr())").Scan(&serverAddress)
	}
	if closeErr := conn.Close(ctx); closeErr != nil || readErr != nil {
		return physicalIdentity{}, "", nil, errors.Join(readErr, closeErr)
	}
	return id, serverAddress, physicalAddresses, nil
}

// attestReaderMember runs the configured reader role attestation on a
// requalification connection. A nil attestor preserves the legacy contract.
func (a *Access) attestReaderMember(ctx context.Context, conn *pgx.Conn) error {
	if a == nil || a.readerRoleAttestor == nil {
		return nil
	}
	return a.readerRoleAttestor(ctx, conn.PgConn())
}

// publishRequalifiedIdentity verifies a freshly read member identity against
// the published writer lineage and publishes it as the new frozen pinning.
// Only a standby proving the lined-up system identifier and database is
// accepted; anything else, an unchanged read, and an out-of-range member
// leave the pinning untouched and report false.
func (a *Access) publishRequalifiedIdentity(member int, id physicalIdentity, serverAddress string, addresses []net.IP) bool {
	if a == nil || member < 0 || member >= len(a.readerMembers) {
		return false
	}
	if a.lineage.isLatched() {
		return false
	}
	if err := validateReaderMemberIdentity(id, a.lineage.identity().physicalIdentity, serverAddress, addresses); err != nil {
		return false
	}
	m := &a.readerMembers[member]
	if pinned := m.currentIdentity(); pinned != nil && pinned.incarnation == id.incarnation && sameAddresses(pinned.addresses, addresses) {
		return false
	}
	m.identity.Store(&memberIdentity{incarnation: id.incarnation, addresses: addresses, serverIP: net.ParseIP(serverAddress)})
	if logger := a.logger; logger != nil {
		logger.InfoContext(context.Background(), "postgres reader member requalified",
			slog.String("event_name", "postgres.reader.member_requalified"),
			slog.Int("member_ordinal", m.ordinal),
			slog.String("member_id", m.id),
			slog.String("incarnation", id.incarnation))
	}
	return true
}

// sameAddresses reports whether two resolved address sets carry the same IPs.
func sameAddresses(left, right []net.IP) bool {
	if len(left) != len(right) {
		return false
	}
	for _, want := range left {
		found := false
		for _, got := range right {
			if want.Equal(got) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
