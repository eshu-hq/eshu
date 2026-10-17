// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

export class Greeter {
  static create() {
    return new Greeter();
  }

  greet(name) {
    return "hello " + name;
  }
}

export function greetAll(names) {
  return names.map((name) => new Greeter().greet(name));
}
