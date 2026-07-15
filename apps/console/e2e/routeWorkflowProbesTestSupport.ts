import { vi } from "vitest";

export interface LocatorStub {
  allTextContents: ReturnType<typeof vi.fn>;
  getAttribute: ReturnType<typeof vi.fn>;
  getByRole: ReturnType<typeof vi.fn>;
  count: ReturnType<typeof vi.fn>;
  click: ReturnType<typeof vi.fn>;
  fill: ReturnType<typeof vi.fn>;
  inputValue: ReturnType<typeof vi.fn>;
  isVisible: ReturnType<typeof vi.fn>;
  nth: ReturnType<typeof vi.fn>;
  textContent: ReturnType<typeof vi.fn>;
  waitFor: ReturnType<typeof vi.fn>;
}

export interface ResponseStub {
  request: () => { method: () => string };
  status: () => number;
  url: () => string;
}

export function locatorStub(overrides: Partial<LocatorStub> = {}): LocatorStub {
  const stub: LocatorStub = {
    allTextContents: vi.fn().mockResolvedValue([]),
    count: vi.fn().mockResolvedValue(1),
    getAttribute: vi.fn().mockResolvedValue(null),
    getByRole: vi.fn(),
    click: vi.fn().mockResolvedValue(undefined),
    fill: vi.fn().mockResolvedValue(undefined),
    inputValue: vi.fn().mockResolvedValue(""),
    isVisible: vi.fn().mockResolvedValue(true),
    nth: vi.fn(),
    textContent: vi.fn().mockResolvedValue("live route content"),
    waitFor: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  };
  if (overrides.nth === undefined) stub.nth.mockImplementation(() => stub);
  return stub;
}
