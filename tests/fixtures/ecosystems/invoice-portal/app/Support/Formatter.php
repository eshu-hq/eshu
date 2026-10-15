<?php
// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

namespace App\Support;

// The application's own Formatter: same short name and method as ledger-kit's.
// App\ is this repository's own autoload namespace, so calls to it get no
// package key and stay in this repository.
class Formatter
{
    public static function formatAmount(int $cents): string
    {
        return (string) $cents;
    }
}
