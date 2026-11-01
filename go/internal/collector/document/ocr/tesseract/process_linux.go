// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tesseract

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
)

// errNonPositiveAddressSpace guards the int64 to uint64 conversion in
// limitAddressSpace. Config validation already rejects such a limit; this
// keeps the helper safe on its own.
var errNonPositiveAddressSpace = errors.New("address-space limit must be positive")

// configureProcess starts the child in its own process group and makes
// cancellation kill the whole group, so an OpenMP worker or helper the
// engine spawns can not outlive the run.
func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
}

// limitAddressSpace sets RLIMIT_AS on the started child. os/exec has no
// pre-exec rlimit hook, so this lands just after execve: the child runs
// uncapped only while the dynamic loader starts, before tesseract loads its
// model or decodes the image. The sandbox cgroup stays the authoritative
// memory ceiling.
func limitAddressSpace(pid int, limit int64) error {
	if limit <= 0 {
		return errNonPositiveAddressSpace
	}
	value := uint64(limit) // #nosec G115 -- limit is checked positive just above
	return unix.Prlimit(pid, unix.RLIMIT_AS, &unix.Rlimit{Cur: value, Max: value}, nil)
}

// sampleHighWaterRSS reads VmHWM, the child's own peak resident set, from
// /proc. rusage ru_maxrss is not used: Go spawns with CLONE_VFORK|CLONE_VM,
// and execve folds the shared parent mm's high-water mark into the child's
// ru_maxrss, so it reports the parent's RSS instead of tesseract's.
func sampleHighWaterRSS(pid int) int64 {
	body, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status")
	if err != nil {
		return 0
	}
	return parseVmHWM(body)
}

// terminationSignal names the signal that ended the child, or "" when it
// exited normally.
func terminationSignal(state *os.ProcessState) string {
	if state == nil {
		return ""
	}
	status, ok := state.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() {
		return ""
	}
	return unix.SignalName(status.Signal())
}

// parseVmHWM returns the VmHWM line of a /proc status file in bytes.
func parseVmHWM(status []byte) int64 {
	for _, line := range bytes.Split(status, []byte("\n")) {
		rest, ok := bytes.CutPrefix(line, []byte("VmHWM:"))
		if !ok {
			continue
		}
		fields := bytes.Fields(rest)
		if len(fields) != 2 || string(fields[1]) != "kB" {
			return 0
		}
		kib, err := strconv.ParseInt(string(fields[0]), 10, 64)
		if err != nil || kib < 0 {
			return 0
		}
		return kib * 1024
	}
	return 0
}
