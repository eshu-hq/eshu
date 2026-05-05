//go:build windows

package eshulocal

import (
	"context"
	"fmt"
)

type ManagedPostgres struct {
	DSN        string
	Port       int
	DataDir    string
	SocketDir  string
	SocketPath string
	PID        int
}

func (m *ManagedPostgres) Close() error {
	return nil
}

func StartEmbeddedPostgres(context.Context, Layout) (*ManagedPostgres, error) {
	return nil, fmt.Errorf("embedded postgres local host is not supported on windows yet")
}

func PostgresDSN(string, int) string {
	return ""
}

func LocalQueryProfile() string {
	return "local_lightweight"
}
