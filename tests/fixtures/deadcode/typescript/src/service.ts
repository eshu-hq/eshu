import express from "express";

const router = express.Router();

export interface AccountShape {
  id: string;
}

export type PublicResult<T> = Promise<T>;

export class AccountService {
  find(id: string): AccountShape {
    return { id };
  }
}

export async function bootstrap(): PublicResult<AccountShape> {
  return formatAccount({ id: "acct-1" });
}

function formatAccount(account: AccountShape): AccountShape {
  return account;
}

function unusedTypedHelper(): string {
  return "unused";
}

function saveAccount(req: unknown, res: { json(value: unknown): unknown }) {
  return res.json({ ok: true });
}

function ambiguousDecoratorTarget(): string {
  return "decorated";
}

function Route(_path: string) {
  return function routeDecorator(_value: unknown, _context: unknown): void {};
}

class Controller {
  @Route("/decorated")
  ambiguousDecoratorTarget(): string {
    return ambiguousDecoratorTarget();
  }
}

async function loadPlugin(name: string): Promise<unknown> {
  return import(`./plugins/${name}`);
}

router.post("/accounts", saveAccount);

void Controller;
void loadPlugin;
