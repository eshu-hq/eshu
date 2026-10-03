// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/jackc/pgx/v5"
)

// ReaderMember names one directly reachable physical standby. It contains no
// credentials; ReadDSN supplies the shared database, role, and TLS settings.
type ReaderMember struct {
	ID   string `json:"id"`
	Host string `json:"host"`
	Port uint16 `json:"port"`
}

// Config fixes one API or MCP process's total pool budget across writer and reader.
// Candidates must resolve to one accepted physical primary and its streaming
// standbys; routing does not imply failover or promotion safety.
type Config struct {
	WriterDSN          string
	ReadDSN            string
	ReadMembers        []ReaderMember
	SamePrimary        bool
	WriterMaxOpenConns int
	ReadMaxOpenConns   int
	WriterMaxIdleConns int
	ReadMaxIdleConns   int
	ConnMaxLifetime    time.Duration
	ConnMaxIdleTime    time.Duration
	PingTimeout        time.Duration
	ReplayTimeout      time.Duration
	ExpectedSystemID   string
	// Logger receives the postgres.store.error record for a failed writer-pool
	// driver call. A nil Logger uses slog.Default.
	Logger *slog.Logger
}

// LoadConfig resolves the optional reader endpoint and validates the shared
// budget before opening either pool. An omitted reader DSN uses the writer.
func LoadConfig(getenv func(string) string) (Config, error) {
	if getenv == nil {
		return Config{}, fmt.Errorf("postgres environment reader is required")
	}
	writer := strings.TrimSpace(getenv("ESHU_POSTGRES_DSN"))
	if writer == "" {
		writer = strings.TrimSpace(getenv("ESHU_CONTENT_STORE_DSN"))
	}
	if writer == "" {
		return Config{}, fmt.Errorf("set ESHU_POSTGRES_DSN or ESHU_CONTENT_STORE_DSN")
	}
	base, err := runtimecfg.LoadPostgresConfig(func(key string) string {
		switch key {
		case "ESHU_FACT_STORE_DSN", "ESHU_CONTENT_STORE_DSN":
			return ""
		case "ESHU_POSTGRES_DSN":
			return writer
		default:
			return getenv(key)
		}
	})
	if err != nil {
		return Config{}, fmt.Errorf("invalid PostgreSQL pool configuration; check ESHU_POSTGRES_MAX_OPEN_CONNS, ESHU_POSTGRES_MAX_IDLE_CONNS, ESHU_POSTGRES_CONN_MAX_LIFETIME, ESHU_POSTGRES_CONN_MAX_IDLE_TIME, and ESHU_POSTGRES_PING_TIMEOUT")
	}
	if base.MaxOpenConns < 2 {
		return Config{}, fmt.Errorf("ESHU_POSTGRES_MAX_OPEN_CONNS must be at least 2 for separate reader and writer pools")
	}
	expectedSystemID := strings.TrimSpace(getenv("ESHU_POSTGRES_EXPECTED_SYSTEM_ID"))
	if err := validateExpectedSystemID(expectedSystemID); err != nil {
		return Config{}, err
	}
	read := strings.TrimSpace(getenv("ESHU_POSTGRES_READ_DSN"))
	if read == "" {
		read = writer
	}
	for _, endpoint := range []string{writer, read} {
		if _, err := parsePhysicalEndpoint(endpoint); err != nil {
			return Config{}, err
		}
	}
	members, err := parseReaderMembers(strings.TrimSpace(getenv("ESHU_POSTGRES_READ_MEMBERS")))
	if err != nil {
		return Config{}, err
	}
	if len(members) > 0 {
		readCfg, parseErr := parsePhysicalEndpoint(read)
		if parseErr != nil || read == writer || len(readCfg.Fallbacks) > 0 {
			return Config{}, fmt.Errorf("reader members require a distinct single-host read DSN")
		}
	}
	readOpen := base.MaxOpenConns / 2
	if value := strings.TrimSpace(getenv("ESHU_POSTGRES_READ_MAX_OPEN_CONNS")); value != "" {
		readOpen, err = strconv.Atoi(value)
		if err != nil {
			return Config{}, fmt.Errorf("ESHU_POSTGRES_READ_MAX_OPEN_CONNS must be an integer")
		}
	}
	writeOpen := base.MaxOpenConns - readOpen
	if readOpen < 1 || writeOpen < 1 {
		return Config{}, fmt.Errorf("ESHU_POSTGRES_READ_MAX_OPEN_CONNS must leave at least one connection for each pool")
	}
	if len(members) > 0 && readOpen/len(members) < 4 {
		return Config{}, fmt.Errorf("ESHU_POSTGRES_READ_MAX_OPEN_CONNS must allow four connections per reader member")
	}
	readIdle := base.MaxIdleConns / 2
	if value := strings.TrimSpace(getenv("ESHU_POSTGRES_READ_MAX_IDLE_CONNS")); value != "" {
		readIdle, err = strconv.Atoi(value)
		if err != nil {
			return Config{}, fmt.Errorf("ESHU_POSTGRES_READ_MAX_IDLE_CONNS must be an integer")
		}
		if readIdle < 0 || readIdle > base.MaxIdleConns || readIdle > readOpen || base.MaxIdleConns-readIdle > writeOpen {
			return Config{}, fmt.Errorf("ESHU_POSTGRES_READ_MAX_IDLE_CONNS must fit both pools and the total idle budget")
		}
	} else {
		if readIdle < base.MaxIdleConns-writeOpen {
			readIdle = base.MaxIdleConns - writeOpen
		}
		if readIdle > readOpen {
			readIdle = readOpen
		}
	}
	return Config{
		WriterDSN: writer, ReadDSN: read, ReadMembers: members, SamePrimary: read == writer,
		WriterMaxOpenConns: writeOpen, ReadMaxOpenConns: readOpen,
		WriterMaxIdleConns: base.MaxIdleConns - readIdle, ReadMaxIdleConns: readIdle,
		ConnMaxLifetime: base.ConnMaxLifetime, ConnMaxIdleTime: base.ConnMaxIdleTime,
		PingTimeout: base.PingTimeout, ReplayTimeout: 2 * time.Second,
		ExpectedSystemID: expectedSystemID,
	}, nil
}

func parseReaderMembers(raw string) ([]ReaderMember, error) {
	if raw == "" {
		return nil, nil
	}
	var members []ReaderMember
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&members); err != nil || len(members) < 2 {
		return nil, fmt.Errorf("ESHU_POSTGRES_READ_MEMBERS must be a JSON array of at least two direct members")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("ESHU_POSTGRES_READ_MEMBERS contains trailing data")
	}
	if err := validateReaderMembers(members); err != nil {
		return nil, err
	}
	return members, nil
}

func validateReaderMembers(members []ReaderMember) error {
	if len(members) < 2 {
		return fmt.Errorf("ESHU_POSTGRES_READ_MEMBERS needs at least two direct members")
	}
	ids, endpoints := map[string]bool{}, map[string]bool{}
	for _, member := range members {
		if member.ID == "" || member.Host == "" || member.Port == 0 || strings.ContainsAny(member.Host, "@/: \t\r\n") || strings.ContainsAny(member.ID, "@/: \t\r\n") {
			return fmt.Errorf("ESHU_POSTGRES_READ_MEMBERS contains an invalid direct member")
		}
		key := fmt.Sprintf("%s:%d", member.Host, member.Port)
		if ids[member.ID] || endpoints[key] {
			return fmt.Errorf("ESHU_POSTGRES_READ_MEMBERS contains a duplicate ID or endpoint")
		}
		ids[member.ID], endpoints[key] = true, true
	}
	return nil
}

func parsePhysicalEndpoint(dsn string) (*pgx.ConnConfig, error) {
	parsed, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("invalid PostgreSQL endpoint configuration")
	}
	if parsed.Host == "" || parsed.Database == "" {
		return nil, fmt.Errorf("PostgreSQL endpoint must name a host and database")
	}
	for _, fallback := range parsed.Fallbacks {
		if fallback.Host == "" {
			return nil, fmt.Errorf("PostgreSQL candidate host must not be empty")
		}
	}
	return parsed, nil
}

func validateExpectedSystemID(value string) error {
	if value == "" {
		return nil
	}
	if _, err := strconv.ParseUint(value, 10, 64); err != nil {
		return fmt.Errorf("ESHU_POSTGRES_EXPECTED_SYSTEM_ID must be an unsigned decimal PostgreSQL system identifier")
	}
	return nil
}
