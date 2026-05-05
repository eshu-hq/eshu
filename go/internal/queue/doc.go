// Package queue defines the durable Go data-plane work-item contracts used
// by the projector, reducer, and replay paths.
//
// WorkItem is the storage-neutral row shape, with a status enum that tracks
// pending -> claimed -> running -> (succeeded | retrying -> ... |
// dead_letter) plus a deprecated legacy `failed` state retained for replay.
// Each transition is a method that validates the current status, clones
// the row, and updates retry and visibility state. The Postgres queue
// adapter is the implementation; this package never opens a connection.
package queue
