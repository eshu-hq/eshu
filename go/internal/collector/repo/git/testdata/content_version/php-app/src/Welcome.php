<?php
// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

namespace App;

use Acme\FixtureLib\Greeter;
use function Acme\FixtureLib\shout;

class Welcome
{
    public function run(string $name): string
    {
        $greeter = new Greeter();
        shout($name);
        return $greeter->wave($name);
    }
}
