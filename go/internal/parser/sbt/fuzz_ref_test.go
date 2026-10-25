// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

// This file holds the reference arm of the grammar fuzz. It interleaves
// resolved column-0 dependency statements and a literal scalaVersion with
// allow-listed key-list settings that name either key as a plain reference,
// and with hostile shapes that name a key as a value but may rewrite it. The
// oracle demands that pure references change nothing (rows resolved, the
// literal version and the `%%` hint kept, nothing counted) and that every
// hostile shape denies its key.

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// fzRefElements are list elements; the keys among them are plain
// references.
var fzRefElements = []string{
	"name", "version", "sbtVersion", "scalaVersion", "libraryDependencies",
	"Compile / scalaVersion", "Test / libraryDependencies",
	"BuildInfoKey.action(\"t\")(System.currentTimeMillis)", "\"k\" -> 1",
}

// fzRefHeads are allow-listed key-list statements; %s is the element list.
var fzRefHeads = []string{
	"buildInfoKeys := Seq[BuildInfoKey](%s)", "buildInfoKeys ++= Seq[BuildInfoKey](%s)",
	"Global / excludeLintKeys ++= Set(%s)", "excludeLintKeys := Set(%s)",
	"ThisBuild / excludeLintKeys ++= Set(%s)", "includeLintKeys ++= Set(%s)",
	"lazy val info = project.settings(buildInfoKeys := Seq[BuildInfoKey](%s), buildInfoPackage := \"x\")",
	"organization := \"x\"; excludeLintKeys ++= Set(%s)",
}

// fzRefCase is one generated file and what the oracle expects of it.
type fzRefCase struct {
	text       string
	rows       []fzMemberRow
	lib, scala bool // a hostile shape for that key was written
}

// fzRefBuild generates the reference-arm file for one seed.
func fzRefBuild(seed int64) fzRefCase {
	r := rand.New(rand.NewSource(seed))
	var c fzRefCase
	var out strings.Builder
	stmts := 3 + r.Intn(6)
	scalaAt := r.Intn(stmts)
	for i := 0; i < stmts; i++ {
		if i == scalaAt {
			out.WriteString("scalaVersion := \"2.13.12\"\n")
		}
		switch r.Intn(6) {
		case 0, 1:
			n := len(c.rows) + 1
			binary := r.Intn(2) == 0
			op := "%"
			if binary {
				op = "%%"
			}
			c.rows = append(c.rows, fzMemberRow{name: fmt.Sprintf("g%d.org:a%d", n, n), binary: binary})
			fmt.Fprintf(&out, "libraryDependencies += \"g%d.org\" %s \"a%d\" %% \"1.%d\"\n", n, op, n, n)
		case 2, 3:
			elements := make([]string, 1+r.Intn(4))
			for j := range elements {
				elements[j] = fzRefElements[r.Intn(len(fzRefElements))]
			}
			sep := ", "
			if r.Intn(4) == 0 {
				sep = ",\n  "
			}
			out.WriteString(sprintfOne(fzRefHeads[r.Intn(len(fzRefHeads))], strings.Join(elements, sep)) + "\n")
		case 4:
			out.WriteString(keyListReferences[r.Intn(len(keyListReferences))] + "\n")
		case 5:
			if r.Intn(3) != 0 {
				break
			}
			key := []string{"scalaVersion", "libraryDependencies"}[r.Intn(2)]
			shape := sprintfOne(keyReferenceHostile[r.Intn(len(keyReferenceHostile))], key)
			if strings.HasPrefix(shape, "(") {
				shape = "organization := \"x\"; " + shape
			}
			out.WriteString(shape + "\n")
			// Every key a hostile shape names is denied.
			c.scala = c.scala || strings.Contains(shape, "scalaVersion")
			c.lib = c.lib || strings.Contains(shape, "libraryDependencies")
		}
	}
	c.text = out.String()
	return c
}

// refReport tallies reference-arm violations across seeds.
type refReport struct {
	cases, rows, state, scala, hint, count int
}

// check parses one reference-arm case and records its violations.
func (r *refReport) check(t *testing.T, seed int64, c fzRefCase) {
	t.Helper()
	payload := parseSource(t, c.text)
	state := stateOf(t, payload)
	rows := dependencyRows(t, payload)
	r.rows += len(rows)
	byName := make(map[string]map[string]any, len(rows))
	for _, row := range rows {
		byName[row["name"].(string)] = row
	}
	wantScala := "2.13.12"
	if c.scala {
		wantScala = ""
	}
	if got := state["scala_version"]; got != wantScala {
		r.scala++
		t.Logf("seed %d: scala_version = %v, want %q\n%s", seed, got, wantScala, c.text)
	}
	count, _ := state["unresolved_declaration_count"].(int)
	if hostile := c.lib || c.scala; (hostile && count < 1) || (!hostile && count != 0) {
		r.count++
		t.Logf("seed %d: unresolved_declaration_count = %d, hostile = %v\n%s", seed, count, hostile, c.text)
	}
	wantState := "resolved"
	if c.lib {
		wantState = "partial"
	}
	for _, want := range c.rows {
		row, ok := byName[want.name]
		if !ok || row["dependency_resolution_state"] != wantState {
			r.state++
			t.Logf("seed %d: %s state = %v, want %s\n%s", seed, want.name, row["dependency_resolution_state"], wantState, c.text)
			continue
		}
		_, hinted := row["sbt_binary_artifact"]
		if hinted != (want.binary && !c.scala) {
			r.hint++
			t.Logf("seed %d: %s hint = %v, want %v\n%s", seed, want.name, hinted, want.binary && !c.scala, c.text)
		}
	}
}

// failures is the total of every reference-arm violation.
func (r *refReport) failures() int {
	return r.state + r.scala + r.hint + r.count
}
