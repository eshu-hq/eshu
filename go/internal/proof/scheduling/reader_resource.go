// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const (
	readerResourceMaxBytes = 65536
	readerSampleMaxAge     = 2 * time.Second
)

type readerResourceConfig struct {
	file      string
	readerUID string
	writerUID string
}

func loadReaderResourceConfig() (readerResourceConfig, error) {
	config := readerResourceConfig{
		file:      os.Getenv("ESHU7033_RESOURCE_GATE_FILE"),
		readerUID: os.Getenv("ESHU7033_READER_POD_UID"),
		writerUID: os.Getenv("ESHU7033_WRITER_POD_UID"),
	}
	if !filepath.IsAbs(config.file) || filepath.Clean(config.file) != config.file ||
		config.readerUID == "" || strings.TrimSpace(config.readerUID) != config.readerUID ||
		config.writerUID == "" || strings.TrimSpace(config.writerUID) != config.writerUID ||
		config.readerUID == config.writerUID {
		return readerResourceConfig{}, fmt.Errorf("reader resource gate requires absolute file and distinct pinned Pod UIDs")
	}
	return config, nil
}

func resourceMap(value any, label string) (map[string]any, error) {
	result, ok := value.(map[string]any)
	if !ok || len(result) == 0 {
		return nil, fmt.Errorf("%s is missing or empty", label)
	}
	return result, nil
}

func resourceString(value any, label string) (string, error) {
	result, ok := value.(string)
	if !ok || result == "" || strings.TrimSpace(result) != result {
		return "", fmt.Errorf("%s is missing", label)
	}
	return result, nil
}

func resourceNumber(value any, label string, positive bool) (float64, error) {
	var number float64
	switch typed := value.(type) {
	case json.Number:
		parsed, err := typed.Float64()
		if err != nil {
			return 0, fmt.Errorf("%s is invalid: %w", label, err)
		}
		number = parsed
	case float64:
		number = typed
	case int:
		number = float64(typed)
	case int64:
		number = float64(typed)
	default:
		return 0, fmt.Errorf("%s is missing", label)
	}
	if math.IsNaN(number) || math.IsInf(number, 0) || number < 0 || (positive && number == 0) {
		return 0, fmt.Errorf("%s is not finite and in range", label)
	}
	return number, nil
}

func resourceInteger(value any, label string, positive bool) (int64, error) {
	var result int64
	switch typed := value.(type) {
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil {
			return 0, fmt.Errorf("%s is not an integer: %w", label, err)
		}
		result = parsed
	case int:
		result = int64(typed)
	case int64:
		result = typed
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) || typed != math.Trunc(typed) || typed > 1<<53 {
			return 0, fmt.Errorf("%s is not a precise integer", label)
		}
		result = int64(typed)
	default:
		return 0, fmt.Errorf("%s is missing", label)
	}
	if result < 0 || (positive && result == 0) {
		return 0, fmt.Errorf("%s is outside range", label)
	}
	return result, nil
}

func resourceTime(value any, label string) (time.Time, error) {
	raw, err := resourceString(value, label)
	if err != nil {
		return time.Time{}, err
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s is not RFC3339: %w", label, err)
	}
	_, offset := parsed.Zone()
	if offset != 0 {
		return time.Time{}, fmt.Errorf("%s is not UTC", label)
	}
	return parsed, nil
}

func resourceRatio(usage, limit any, label string) (float64, error) {
	used, err := resourceNumber(usage, label+" usage", false)
	if err != nil {
		return 0, err
	}
	capacity, err := resourceNumber(limit, label+" limit", true)
	if err != nil {
		return 0, err
	}
	return used / capacity, nil
}

func resourceMemoryRatio(usage, limit any, label string) (float64, error) {
	used, err := resourceInteger(usage, label+" usage", false)
	if err != nil {
		return 0, err
	}
	capacity, err := resourceInteger(limit, label+" limit", true)
	if err != nil {
		return 0, err
	}
	return float64(used) / float64(capacity), nil
}

func validateResourceRole(value any, label, expectedUID string, ceilings map[string]float64, now time.Time) error {
	role, err := resourceMap(value, label)
	if err != nil {
		return err
	}
	for _, field := range []string{"pod_name", "namespace", "node_name"} {
		if _, err := resourceString(role[field], label+"."+field); err != nil {
			return err
		}
	}
	windowText, err := resourceString(role["window"], label+".window")
	if err != nil {
		return err
	}
	uid, err := resourceString(role["pod_uid"], label+".pod_uid")
	if err != nil || uid != expectedUID {
		return fmt.Errorf("%s Pod UID does not match fresh preflight", label)
	}
	phase, err := resourceString(role["phase"], label+".phase")
	if err != nil || phase != "Running" {
		return fmt.Errorf("%s Pod is not Running", label)
	}
	metricTime, err := resourceTime(role["timestamp"], label+".timestamp")
	if err != nil || metricTime.After(now) || now.Sub(metricTime).Seconds() > ceilings["metric_age_seconds"] {
		return fmt.Errorf("%s metric timestamp is stale or invalid", label)
	}
	metricAge, err := resourceNumber(role["metric_age_seconds"], label+".metric_age_seconds", false)
	if err != nil || metricAge > ceilings["metric_age_seconds"] {
		return fmt.Errorf("%s metric age is missing or beyond ceiling", label)
	}
	window, err := resourceNumber(role["window_seconds"], label+".window_seconds", true)
	if err != nil || window > ceilings["metric_window_seconds"] {
		return fmt.Errorf("%s metric window is missing or beyond ceiling", label)
	}
	parsedWindow, err := time.ParseDuration(windowText)
	if err != nil || math.Abs(parsedWindow.Seconds()-window) > 0.001 {
		return fmt.Errorf("%s metric window text and seconds disagree", label)
	}
	containers, err := resourceMap(role["containers"], label+".containers")
	if err != nil {
		return err
	}
	for name, value := range containers {
		container, err := resourceMap(value, label+" container "+name)
		if err != nil || name == "" {
			return fmt.Errorf("%s container is missing or invalid", label)
		}
		cpuRatio, err := resourceRatio(container["cpu_cores"], container["cpu_limit_cores"], label+" CPU")
		if err != nil || cpuRatio > ceilings["cpu_ratio"] {
			return fmt.Errorf("%s container CPU exceeds declared ceiling", label)
		}
		memoryRatio, err := resourceMemoryRatio(container["memory_bytes"], container["memory_limit_bytes"], label+" memory")
		if err != nil || memoryRatio > ceilings["memory_ratio"] {
			return fmt.Errorf("%s container memory exceeds declared ceiling", label)
		}
	}
	return nil
}

func validateReaderResourceSample(sample map[string]any, config readerResourceConfig, now time.Time) error {
	sampledAt, err := resourceTime(sample["sample_at"], "sample_at")
	if err != nil || sampledAt.After(now) || now.Sub(sampledAt) > readerSampleMaxAge {
		return fmt.Errorf("watcher sample is missing, future, or older than two seconds")
	}
	if _, err := resourceInteger(sample["sample_seq"], "sample_seq", true); err != nil {
		return fmt.Errorf("watcher sample sequence is invalid")
	}
	breaches, ok := sample["breaches"].([]any)
	if !ok {
		if typed, stringsOK := sample["breaches"].([]string); stringsOK {
			if len(typed) != 0 {
				return fmt.Errorf("watcher reports %d resource breaches", len(typed))
			}
		} else {
			return fmt.Errorf("watcher breach list is missing")
		}
	} else if len(breaches) != 0 {
		return fmt.Errorf("watcher reports %d resource breaches", len(breaches))
	}
	declared, err := resourceMap(sample["ceilings"], "ceilings")
	if err != nil {
		return err
	}
	ceilings := make(map[string]float64, 4)
	for _, key := range []string{"cpu_ratio", "memory_ratio", "metric_age_seconds", "metric_window_seconds"} {
		value, err := resourceNumber(declared[key], "ceilings."+key, true)
		if err != nil {
			return err
		}
		ceilings[key] = value
	}
	if ceilings["cpu_ratio"] > 1 || ceilings["memory_ratio"] > 1 ||
		ceilings["metric_age_seconds"] > 300 || ceilings["metric_window_seconds"] > 3600 {
		return fmt.Errorf("watcher declared ceilings are outside safe bounds")
	}
	roles, err := resourceMap(sample["roles"], "roles")
	if err != nil {
		return err
	}
	for _, role := range []struct{ name, uid string }{{"reader", config.readerUID}, {"writer", config.writerUID}} {
		if err := validateResourceRole(roles[role.name], role.name, role.uid, ceilings, now); err != nil {
			return err
		}
	}
	return nil
}

func checkReaderResourceFile(ctx context.Context, config readerResourceConfig) (resultErr error) {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("reader resource file context: %w", err)
	}
	fd, err := unix.Open(config.file, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open reader resource watcher: %w", err)
	}
	var info unix.Stat_t
	if err := unix.Fstat(fd, &info); err != nil || info.Mode&unix.S_IFMT != unix.S_IFREG || info.Size < 1 || info.Size > readerResourceMaxBytes {
		_ = unix.Close(fd)
		return fmt.Errorf("reader resource watcher file is not a bounded regular file")
	}
	file := os.NewFile(uintptr(fd), config.file)
	if file == nil {
		_ = unix.Close(fd)
		return fmt.Errorf("wrap reader resource watcher file")
	}
	defer func() {
		resultErr = errors.Join(resultErr, file.Close())
	}()
	data, err := io.ReadAll(io.LimitReader(file, readerResourceMaxBytes+1))
	if err != nil || len(data) > readerResourceMaxBytes {
		return fmt.Errorf("read reader resource watcher: %v", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var sample map[string]any
	if err := decoder.Decode(&sample); err != nil {
		return fmt.Errorf("decode reader resource watcher: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("reader resource watcher contains trailing JSON")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("reader resource validation context: %w", err)
	}
	return validateReaderResourceSample(sample, config, time.Now().UTC())
}
