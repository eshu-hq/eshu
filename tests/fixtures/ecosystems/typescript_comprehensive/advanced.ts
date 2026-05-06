/**
 * Advanced TypeScript patterns for parser testing.
 */

// Enums
enum Direction {
    Up = "UP",
    Down = "DOWN",
    Left = "LEFT",
    Right = "RIGHT",
}

const enum Color {
    Red = 0,
    Green = 1,
    Blue = 2,
}

enum HttpStatus {
    OK = 200,
    NotFound = 404,
    InternalError = 500,
}

// Namespace
namespace Validators {
    export interface StringValidator {
        isValid(s: string): boolean;
    }

    export class EmailValidator implements StringValidator {
        isValid(s: string): boolean {
            return s.includes("@");
        }
    }

    export class UrlValidator implements StringValidator {
        isValid(s: string): boolean {
            return s.startsWith("http");
        }
    }
}

// Declaration merging
interface Box {
    height: number;
    width: number;
}

interface Box {
    depth: number;
}

function createBox(): Box {
    return { height: 1, width: 1, depth: 1 };
}

export { Direction, Color, HttpStatus, Validators, createBox };
