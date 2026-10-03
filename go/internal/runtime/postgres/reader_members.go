// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
)

type physicalReaderMember struct {
	ordinal     int
	id          string
	host        string
	pool        *sql.DB
	incarnation string
	serverIP    net.IP
	addresses   []net.IP
	maxOpen     int
}

func openReaderMembers(ctx context.Context, stageBudget time.Duration, cfg Config, template *pgx.ConnConfig, writer physicalIdentity) ([]physicalReaderMember, error) {
	if len(cfg.ReadMembers) < 2 || cfg.SamePrimary || len(template.Fallbacks) != 0 || cfg.ReadMaxOpenConns/len(cfg.ReadMembers) < 4 {
		return nil, errors.New("invalid physical reader member configuration")
	}
	return qualifyReaderCandidates(ctx, cfg.ReadMembers, stageBudget, func(memberCtx context.Context, index int, member ReaderMember) (physicalReaderMember, error) {
		return qualifyReaderMember(memberCtx, cfg, template, writer, index, member)
	})
}

func qualifyReaderMember(ctx context.Context, cfg Config, template *pgx.ConnConfig, writer physicalIdentity, index int, member ReaderMember) (physicalReaderMember, error) {
	config := template.Copy()
	originalDialFunc := config.DialFunc
	dialTracker := &bootstrapDialTracker{}
	config.DialFunc = dialTracker.wrap(originalDialFunc)
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
		return physicalReaderMember{}, fmt.Errorf("reader member DNS: %w", err)
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
		return physicalReaderMember{}, errors.Join(fmt.Errorf("reader member connect: %w", err), dialTracker.closeAll())
	}
	id, readErr := readPhysicalPGX(ctx, conn)
	var serverAddress string
	if readErr == nil {
		readErr = conn.QueryRow(ctx, "SELECT host(inet_server_addr())").Scan(&serverAddress)
	}
	closeErr := conn.Close(ctx)
	trackerErr := dialTracker.closeAll()
	cleanupErr := awaitPGXCleanup(ctx, conn.PgConn().CleanupDone(), dialTracker.closeAll)
	if err := readerMemberQualificationError(readErr, id, writer, serverAddress, physicalAddresses, closeErr, trackerErr, cleanupErr); err != nil {
		return physicalReaderMember{}, err
	}
	config.DialFunc = originalDialFunc
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
	return physicalReaderMember{ordinal: index, id: member.ID, host: member.Host, pool: pool, incarnation: id.incarnation, serverIP: net.ParseIP(serverAddress), addresses: physicalAddresses, maxOpen: maxOpen}, nil
}

func awaitPGXCleanup(ctx context.Context, cleanupDone <-chan struct{}, forceClose func() error) error {
	select {
	case <-cleanupDone:
		return nil
	default:
	}
	if ctx.Err() != nil {
		closeErr := forceClose()
		<-cleanupDone
		return errors.Join(ctx.Err(), closeErr)
	}
	<-cleanupDone
	return nil
}

func validateReaderMemberIdentity(id, writer physicalIdentity, serverAddress string, addresses []net.IP) error {
	if id.recovery != "true" || id.readOnly != "on" || id.defaultReadOnly != "on" || id.systemID != writer.systemID || id.database != writer.database || id.incarnation == "" || !addressMatches(serverAddress, addresses) {
		return ErrWrongTopology
	}
	return nil
}

func readerMemberQualificationError(readErr error, id, writer physicalIdentity, serverAddress string, addresses []net.IP, cleanupErrors ...error) error {
	var identityErr error
	if readErr == nil {
		identityErr = validateReaderMemberIdentity(id, writer, serverAddress, addresses)
	}
	return errors.Join(append([]error{readErr, identityErr}, cleanupErrors...)...)
}

type readerCandidateResult struct {
	ordinal int
	member  physicalReaderMember
	err     error
}

func qualifyReaderCandidates(ctx context.Context, inventory []ReaderMember, attemptTimeout time.Duration, qualify func(context.Context, int, ReaderMember) (physicalReaderMember, error)) ([]physicalReaderMember, error) {
	var workCtx context.Context
	var cancel context.CancelFunc
	if attemptTimeout > 0 {
		workCtx, cancel = context.WithTimeout(ctx, attemptTimeout)
	} else {
		workCtx, cancel = context.WithCancel(ctx)
	}
	defer cancel()
	results := make(chan readerCandidateResult, len(inventory))
	var workers sync.WaitGroup
	for ordinal, member := range inventory {
		ordinal, member := ordinal, member
		workers.Add(1)
		go func() {
			defer workers.Done()
			qualified, err := qualify(workCtx, ordinal, member)
			if err != nil && transientReaderMemberFailure(ctx, err) {
				err = fmt.Errorf("reader member qualification: %w", errors.Join(ErrReaderUnavailable, err))
			}
			results <- readerCandidateResult{ordinal: ordinal, member: qualified, err: err}
		}()
	}
	qualified := make([]readerCandidateResult, 0, len(inventory))
	var failures error
	var fatal error
	for range inventory {
		result := <-results
		qualified = append(qualified, result)
		if result.err != nil {
			if errors.Is(result.err, ErrReaderUnavailable) {
				failures = errors.Join(failures, result.err)
			} else {
				fatal = errors.Join(fatal, result.err)
				cancel()
			}
		}
	}
	workers.Wait()
	if fatal != nil {
		var opened []physicalReaderMember
		for _, result := range qualified {
			if result.err == nil {
				opened = append(opened, result.member)
			}
		}
		return nil, errors.Join(fatal, closeReaderMembers(opened))
	}
	members := make([]physicalReaderMember, 0, len(qualified))
	for _, result := range qualified {
		if result.err == nil {
			members = append(members, result.member)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(err, failures, closeReaderMembers(members))
	}
	if len(members) == 0 {
		return nil, errors.Join(ErrReaderUnavailable, failures, ctx.Err())
	}
	sort.Slice(members, func(i, j int) bool { return members[i].ordinal < members[j].ordinal })
	for i := range members {
		for j := 0; j < i; j++ {
			if members[i].serverIP != nil && members[i].serverIP.Equal(members[j].serverIP) && members[i].incarnation == members[j].incarnation {
				return nil, errors.Join(ErrWrongTopology, closeReaderMembers(members))
			}
		}
	}
	return members, nil
}

func transientReaderMemberFailure(parent context.Context, err error) bool {
	if err == nil || parent.Err() != nil {
		return false
	}
	return readerFailureClass(err) == readerFailureTransient
}

type readerFailureKind uint8

const (
	readerFailureFatal readerFailureKind = iota
	readerFailureNeutral
	readerFailureTransient
	readerFailureCanceled
)

// A joined transport timeout must not mask a sibling authentication, TLS, or
// malformed-metadata failure. Only a tree of known transient causes may be
// skipped; sentinels are neutral when paired with an actual transient cause.
func readerFailureClass(err error) readerFailureKind {
	if err == nil {
		return readerFailureNeutral
	}
	if pgErr, ok := err.(*pgconn.PgError); ok {
		switch pgErr.Code {
		case "08001", "08003", "08006", "57P01", "57P02", "57P03", "53300":
			return readerFailureTransient
		default:
			return readerFailureFatal
		}
	}
	if _, ok := err.(*net.DNSError); ok {
		return readerFailureTransient
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		kind := readerFailureNeutral
		for _, cause := range joined.Unwrap() {
			child := readerFailureClass(cause)
			if child == readerFailureFatal {
				return readerFailureFatal
			}
			if child == readerFailureCanceled {
				kind = readerFailureCanceled
			} else if child == readerFailureTransient && kind == readerFailureNeutral {
				kind = readerFailureTransient
			}
		}
		return kind
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return readerFailureClass(wrapped.Unwrap())
	}
	for _, neutral := range []error{ErrReaderUnavailable, ErrReaderStale, errReaderPermitTimeout} {
		if errors.Is(err, neutral) {
			return readerFailureNeutral
		}
	}
	if errors.Is(err, context.Canceled) {
		return readerFailureCanceled
	}
	for _, transient := range []error{context.DeadlineExceeded, io.EOF, io.ErrUnexpectedEOF, syscall.ECONNREFUSED, syscall.ECONNRESET, syscall.ECONNABORTED, syscall.ENETUNREACH, syscall.EHOSTUNREACH, syscall.ETIMEDOUT} {
		if errors.Is(err, transient) {
			return readerFailureTransient
		}
	}
	return readerFailureFatal
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
			return memberLocalTopology{}
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
		if member.pool != nil {
			result = errors.Join(result, member.pool.Close())
		}
	}
	return result
}

func (a *Access) memberOrder(count int) []int {
	if len(a.readerMembers) == 0 {
		return nil
	}
	// #nosec G115 -- modulo a nonempty slice length bounds the result to an int index.
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
