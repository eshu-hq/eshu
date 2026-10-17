// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

import { Greeter } from "acme-fixture-lib";

export function welcome(name) {
  Greeter.create();
  return new Greeter().greet(name);
}
