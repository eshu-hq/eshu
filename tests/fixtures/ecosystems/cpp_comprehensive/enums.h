#ifndef ENUMS_H
#define ENUMS_H

#include <string>

// C-style enum
enum OldColor {
    RED,
    GREEN,
    BLUE
};

// Scoped enum (enum class)
enum class Direction {
    North,
    South,
    East,
    West
};

// Enum class with underlying type
enum class HttpStatus : int {
    OK = 200,
    NotFound = 404,
    InternalError = 500
};

// Helper function for enum
std::string directionToString(Direction dir);
int statusCode(HttpStatus status);

#endif // ENUMS_H
