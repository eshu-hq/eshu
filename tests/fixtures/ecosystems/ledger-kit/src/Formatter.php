<?php
// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

namespace Acme\LedgerKit;

// formatAmount is called nowhere in this repository; only invoice-portal calls
// it, through the cross-repository package key.
class Formatter
{
    public static function formatAmount(int $cents): string
    {
        return self::pad(intdiv($cents, 100)) . '.' . self::pad($cents % 100);
    }

    // A private helper: only a public static method gets a package key, so
    // pad stays unkeyed.
    private static function pad(int $value): string
    {
        return str_pad((string) $value, 2, '0', STR_PAD_LEFT);
    }
}
