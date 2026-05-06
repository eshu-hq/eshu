package main

import (
	"io"
	"net"
	"testing"
	"time"
)

func TestGraphBoltHealthyRejectsNoVersionSelected(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() {
		_ = listener.Close()
	})

	done := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		defer func() {
			_ = conn.Close()
		}()
		if _, err := io.ReadFull(conn, make([]byte, len(graphBoltHandshake))); err != nil {
			done <- err
			return
		}
		_, err = conn.Write([]byte{0, 0, 0, 0})
		done <- err
	}()

	addr := listener.Addr().(*net.TCPAddr)
	if graphBoltHealthy("127.0.0.1", addr.Port, time.Second) {
		t.Fatal("graphBoltHealthy() = true, want false for no-version response")
	}
	if err := <-done; err != nil {
		t.Fatalf("server probe: %v", err)
	}
}
