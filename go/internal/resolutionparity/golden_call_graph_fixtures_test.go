// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package resolutionparity

import "github.com/eshu-hq/eshu/go/internal/codeprovenance"

type goldenCallGraphFixture struct {
	language  string
	files     map[string]string
	caller    string
	callee    string
	method    codeprovenance.Method
	uidByPath map[string]string
}

var sourceCallGraphFixtures = []goldenCallGraphFixture{
	{
		language: "c",
		files: map[string]string{"main.c": `
void helper(void) {}
void caller(void) {
  helper();
}
`},
		caller: "caller", callee: "helper", method: codeprovenance.MethodSameFile,
	},
	{
		language: "c_sharp",
		files: map[string]string{"Program.cs": `
class Program {
  static void helper() {}
  static void caller() {
    helper();
  }
}
`},
		caller: "caller", callee: "helper", method: codeprovenance.MethodSameFile,
	},
	{
		language: "cpp",
		files: map[string]string{"main.cpp": `
void helper() {}
void caller() {
  helper();
}
`},
		caller: "caller", callee: "helper", method: codeprovenance.MethodSameFile,
	},
	{
		language: "dart",
		files: map[string]string{"main.dart": `
class Helper {}
class Caller { final value = Helper(); }
`},
		caller: "Caller", callee: "Helper", method: codeprovenance.MethodSameFile,
	},
	{
		language: "elixir",
		files: map[string]string{"sample.ex": `
defmodule Demo.Basic do
  def greet do
    :ok
  end
end

defmodule Demo.Worker do
  def caller do
    Demo.Basic.greet()
  end
end
`},
		caller: "caller", callee: "greet", method: codeprovenance.MethodSameFile,
	},
	{
		language: "go",
		files: map[string]string{"main.go": `
package main

func helper() {}

func caller() {
	helper()
}
`},
		caller: "caller", callee: "helper", method: codeprovenance.MethodSameFile,
	},
	{
		language: "groovy",
		files: map[string]string{"script.groovy": `
def helper() {
}

def caller() { helper() }
`},
		caller: "caller", callee: "helper", method: codeprovenance.MethodSameFile,
	},
	{
		language: "haskell",
		files: map[string]string{"Main.hs": `
helper x = x
caller x = helper x
`},
		caller: "caller", callee: "helper", method: codeprovenance.MethodSameFile,
	},
	{
		language: "java",
		files: map[string]string{"Sample.java": `
class Sample {
  void helper() {}
  void caller() {
    helper();
  }
}
`},
		caller: "caller", callee: "helper", method: codeprovenance.MethodSameFile,
	},
	{
		language: "javascript",
		files: map[string]string{"main.js": `
function helper() {}
function caller() {
  helper();
}
`},
		caller: "caller", callee: "helper", method: codeprovenance.MethodSameFile,
	},
	{
		language: "kotlin",
		files: map[string]string{"Worker.kt": `
package comprehensive

class Worker {
    fun helper(): String = "ok"
    fun caller(): String = this.helper()
}
`},
		caller: "caller", callee: "helper", method: codeprovenance.MethodSameFile,
	},
	{
		language: "perl",
		files: map[string]string{"main.pl": `
sub helper {}
sub caller { helper(); }
`},
		caller: "caller", callee: "helper", method: codeprovenance.MethodSameFile,
	},
	{
		language: "php",
		files: map[string]string{"main.php": `<?php
class Service {
    public function helper(): string {
        return "ok";
    }
}

class Worker {
    private Service $service;
    public function caller(): string { return $this->service->helper(); }
}
`},
		caller: "caller", callee: "helper", method: codeprovenance.MethodSameFile,
	},
	{
		language: "python",
		files: map[string]string{"main.py": `
def helper():
    return None

def caller():
    return helper()
`},
		caller: "caller", callee: "helper", method: codeprovenance.MethodSameFile,
	},
	{
		language: "ruby",
		files: map[string]string{"main.rb": `
class ApiController
  def caller
    value = build_scopes
  end

  private

  def build_scopes
    []
  end
end
`},
		caller: "caller", callee: "build_scopes", method: codeprovenance.MethodSameFile,
	},
	{
		language: "rust",
		files: map[string]string{"main.rs": `
fn helper() {}
fn caller() {
    helper();
}
`},
		caller: "caller", callee: "helper", method: codeprovenance.MethodSameFile,
	},
	{
		language: "swift",
		files: map[string]string{"main.swift": `
func helper() {}
struct Caller { let value = helper() }
`},
		caller: "Caller", callee: "helper", method: codeprovenance.MethodSameFile,
	},
	{
		language: "scala",
		files: map[string]string{"Sample.scala": `
object Sample {
  def helper(): Unit = {}
  def caller(): Unit = {
    helper()
  }
}
`},
		caller: "caller", callee: "helper", method: codeprovenance.MethodSameFile,
	},
	{
		language: "tsx",
		files: map[string]string{"main.tsx": `
export function helper(): number {
  return 1;
}

export function caller(): number {
  return helper();
}
`},
		caller: "caller", callee: "helper", method: codeprovenance.MethodSameFile,
	},
	{
		language: "typescript",
		files: map[string]string{"main.ts": `
export function helper(): number {
  return 1;
}

export function caller(): number {
  return helper();
}
`},
		caller: "caller", callee: "helper", method: codeprovenance.MethodSameFile,
	},
}

var sourceCallGraphFixtureGaps = map[string]string{}
