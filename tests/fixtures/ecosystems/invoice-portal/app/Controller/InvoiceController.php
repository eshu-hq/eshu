<?php
// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

namespace App\Controller;

use Acme\LedgerKit\Formatter;
use Acme\LedgerKit\Money;
use Symfony\Component\Routing\Attribute\Route;

class InvoiceController
{
    // A Symfony route action, so a reachability root. Formatter::formatAmount is
    // the cross-repository edge; Money is only a type hint and an instanceof
    // check; \App\Support\Formatter::formatAmount is the application's own class.
    #[Route('/invoices/{id}', methods: ['GET'])]
    public function show(int $id, Money $total): string
    {
        $label = Formatter::formatAmount($id);
        if ($total instanceof Money) {
            $label .= ' ' . \App\Support\Formatter::formatAmount($total->cents);
        }
        return $label;
    }
}
