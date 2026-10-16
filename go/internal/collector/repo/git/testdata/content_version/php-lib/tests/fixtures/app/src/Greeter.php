<?php
// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

namespace Acme\FixtureLib;

class Greeter
{
    public static function greet(string $name): string
    {
        return "hello " . $name;
    }
}
