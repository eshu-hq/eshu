<?php
// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

namespace Acme\LedgerKit;

// Money appears in invoice-portal only as a type hint and an instanceof check,
// never as a call.
class Money
{
    public function __construct(public int $cents)
    {
    }
}
