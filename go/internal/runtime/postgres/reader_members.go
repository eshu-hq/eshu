// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
)

type physicalReaderMember struct {
	id          string
	host        string
	pool        *sql.DB
	incarnation string
	serverIP    net.IP
	addresses   []net.IP
	maxOpen     int
}

func openReaderMembers(ctx context.Context, cfg Config, template *pgx.ConnConfig, writer physicalIdentity) ([]physicalReaderMember, error) {
	if len(cfg.ReadMembers) < 2 || cfg.SamePrimary || len(template.Fallbacks) != 0 || cfg.ReadMaxOpenConns/len(cfg.ReadMembers) < 4 {
		return nil, errors.New("invalid physical reader member configuration")
	}
	members := make([]physicalReaderMember, 0, len(cfg.ReadMembers))
	var unavailable error
	for index, member := range cfg.ReadMembers {
		config := template.Copy()
		config.Host, config.Port, config.Fallbacks = member.Host, member.Port, nil
		if config.TLSConfig != nil {
			config.TLSConfig = config.TLSConfig.Clone()
			config.TLSConfig.ServerName = member.Host
		}
		if config.RuntimeParams == nil {
			config.RuntimeParams = map[string]string{}
		}
		config.RuntimeParams["default_transaction_read_only"] = "on"
		addresses, err := net.DefaultResolver.LookupIPAddr(ctx, member.Host)
		if err != nil || len(addresses) == 0 {
			if err == nil {
				err = errors.New("no member addresses")
			}
			unavailable = errors.Join(unavailable, fmt.Errorf("reader member %s DNS: %w", member.ID, err))
			continue // An unavailable member may rejoin after an Access restart.
		}
		physicalAddresses := make([]net.IP, 0, len(addresses))
		for _, address := range addresses {
			physicalAddresses = append(physicalAddresses, address.IP)
		}
		// Inspect the physical endpoint ourselves at bootstrap. A pgx role
		// validator can reject a primary with a generic connect error, which
		// would incorrectly treat a wrong member as merely unavailable.
		config.ValidateConnect = nil
		conn, err := pgx.ConnectConfig(ctx, config)
		if err != nil {
			unavailable = errors.Join(unavailable, fmt.Errorf("reader member %s connect: %w", member.ID, err))
			if errors.Is(err, ErrWrongTopology) {
				return nil, errors.Join(ErrWrongTopology, closeReaderMembers(members))
			}
			continue
		}
		id, readErr := readPhysicalPGX(ctx, conn)
		var serverAddress string
		if readErr == nil {
			readErr = conn.QueryRow(ctx, "SELECT host(inet_server_addr())").Scan(&serverAddress)
		}
		closeErr := conn.Close(ctx)
		if readErr != nil || closeErr != nil || id.recovery != "true" || id.readOnly != "on" || id.defaultReadOnly != "on" || id.systemID != writer.systemID || id.database != writer.database || id.incarnation == "" || !addressMatches(serverAddress, physicalAddresses) {
			return nil, errors.Join(ErrWrongTopology, readErr, closeErr, closeReaderMembers(members))
		}
		for _, prior := range members {
			if prior.serverIP.Equal(net.ParseIP(serverAddress)) && prior.incarnation == id.incarnation {
				return nil, errors.Join(ErrWrongTopology, closeReaderMembers(members))
			}
		}
		config.ValidateConnect = memberValidator(writer, id.incarnation, physicalAddresses)
		pool := stdlib.OpenDB(*config)
		maxOpen := cfg.ReadMaxOpenConns / len(cfg.ReadMembers)
		if index < cfg.ReadMaxOpenConns%len(cfg.ReadMembers) {
			maxOpen++
		}
		maxIdle := cfg.ReadMaxIdleConns / len(cfg.ReadMembers)
		if index < cfg.ReadMaxIdleConns%len(cfg.ReadMembers) {
			maxIdle++
		}
		if maxIdle > maxOpen {
			maxIdle = maxOpen
		}
		pool.SetMaxOpenConns(maxOpen)
		pool.SetMaxIdleConns(maxIdle)
		pool.SetConnMaxLifetime(cfg.ConnMaxLifetime)
		pool.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)
		members = append(members, physicalReaderMember{id: member.ID, host: member.Host, pool: pool, incarnation: id.incarnation, serverIP: net.ParseIP(serverAddress), addresses: physicalAddresses, maxOpen: maxOpen})
	}
	if len(members) == 0 {
		return nil, errors.Join(ErrReaderUnavailable, unavailable)
	}
	return members, nil
}

func memberValidator(writer physicalIdentity, incarnation string, addresses []net.IP) pgconn.ValidateConnectFunc {
	base := readerValidator(writer, false)
	return func(ctx context.Context, conn *pgconn.PgConn) error {
		if err := base(ctx, conn); err != nil {
			return err
		}
		id, err := readPhysicalRaw(ctx, conn)
		if err != nil {
			return err
		}
		results, err := conn.Exec(ctx, "SELECT host(inet_server_addr())").ReadAll()
		if err != nil {
			return err
		}
		if id.incarnation != incarnation || len(results) != 1 || len(results[0].Rows) != 1 || len(results[0].Rows[0]) != 1 || !addressMatches(string(results[0].Rows[0][0]), addresses) {
			return ErrWrongTopology
		}
		return nil
	}
}

func addressMatches(actual string, expected []net.IP) bool {
	address := net.ParseIP(actual)
	for _, candidate := range expected {
		if address != nil && address.Equal(candidate) {
			return true
		}
	}
	return false
}

func closeReaderMembers(members []physicalReaderMember) error {
	var result error
	for _, member := range members {
		result = errors.Join(result, member.pool.Close())
	}
	return result
}

func (a *Access) memberOrder(count int) []int {
	if len(a.readerMembers) == 0 {
		return nil
	}
	start := int((a.nextReader.Add(1) - 1) % uint64(len(a.readerMembers)))
	order := make([]int, 0, len(a.readerMembers))
	for offset := range a.readerMembers {
		index := (start + offset) % len(a.readerMembers)
		if a.readerMembers[index].maxOpen >= count {
			order = append(order, index)
		}
	}
	return order
}

func (a *Access) aggregateReaderStats() sql.DBStats {
	if len(a.readerMembers) == 0 {
		return a.reader.Stats()
	}
	var total sql.DBStats
	for _, member := range a.readerMembers {
		stats := member.pool.Stats()
		total.MaxOpenConnections += stats.MaxOpenConnections
		total.OpenConnections += stats.OpenConnections
		total.InUse += stats.InUse
		total.Idle += stats.Idle
		total.WaitCount += stats.WaitCount
		total.WaitDuration += stats.WaitDuration
		total.MaxIdleClosed += stats.MaxIdleClosed
		total.MaxIdleTimeClosed += stats.MaxIdleTimeClosed
		total.MaxLifetimeClosed += stats.MaxLifetimeClosed
	}
	return total
}
