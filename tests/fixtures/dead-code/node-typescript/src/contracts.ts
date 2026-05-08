export interface ResponseAdapter {
  createResponse(input: string): string;
}

export class JsonResponseAdapter implements ResponseAdapter {
  createResponse(input: string): string {
    return JSON.stringify({ input });
  }
}

export function validatePayload(input: unknown): boolean {
  return typeof input === "object" && input !== null;
}
