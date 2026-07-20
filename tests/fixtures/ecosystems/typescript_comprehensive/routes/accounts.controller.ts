// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// #5361 route query-proof matrix: cribs the NestJS controller shape proven
// by internal/parser/engine_javascript_koa_fastify_nestjs_route_entries_test.go.
import { Controller, Get, Post } from "@nestjs/common";

@Controller("accounts")
export class AccountsController {
  @Get(":id")
  getAccount() {
    return {};
  }

  @Post()
  createAccount() {
    return {};
  }
}
