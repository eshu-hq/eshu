// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package config owns the logic behind the `eshu config` command family: the
// persisted settings file every other CLI family resolves values from, the
// graph-backend selection written by `eshu config db`, and the ESHU_*
// environment check behind `eshu config validate`.
//
// # Settings file
//
// Settings live in one key=value file, .env, inside the Eshu config
// directory. Home resolves that directory and is the only place in this
// package that reads the process environment: it consults ESHU_HOME, expanding
// a leading "~" against the user's home directory, and falls back to ~/.eshu.
// No other environment variable is read here or anywhere in this package's
// dependency closure -- internal/envregistry, the only non-standard-library
// package imported, holds a static registry and reads no environment of its
// own; the environment it validates is passed in as a map.
//
// Load, ResolveValue, SetValue, and Reset are the read/write surface over that
// file. ResolveValue is the one the rest of the CLI depends on: `eshu`'s API
// client, doctor, vuln-scan, first-run diagnostics, and the config commands
// themselves all resolve settings through it, with an optional profile name
// selecting a "<KEY>_<PROFILE>" variant ahead of the bare key.
//
// # Ownership boundary
//
// This package holds no process wiring. It reads no cobra flags, resolves no
// cobra streams, calls neither os.Environ nor os.Exit, and prints nothing to
// the process's stdout or stderr -- ValidateEnv writes only to the io.Writer
// its caller supplies. Its file access is confined to the .env file under Home.
//
// go/cmd/eshu is package main, so nothing can import it; every symbol that
// reads a flag, reads real stdin, prints to the real stdout, or maps a result
// to an exit code therefore stays in the cobra wrappers config_cmd.go and
// config_validate.go. Those wrappers resolve process state -- the --strict and
// --reference flags, cmd.OutOrStdout, os.Environ, the reset confirmation read
// from stdin -- and pass it in here as plain values.
package config
