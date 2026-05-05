#ifndef STRUCTS_H
#define STRUCTS_H

#include <string>
#include <variant>

// Basic struct
struct Point {
    double x;
    double y;

    double distance(const Point& other) const;
};

// Struct with methods
struct Config {
    std::string host = "localhost";
    int port = 8080;
    bool debug = false;

    std::string url() const;
};

// Union
union DataValue {
    int intVal;
    float floatVal;
    char charVal;
};

// Nested types
struct Response {
    struct Header {
        std::string key;
        std::string value;
    };

    int statusCode;
    std::vector<Header> headers;
    std::string body;
};

// std::variant (modern union)
using Value = std::variant<int, double, std::string>;

#endif // STRUCTS_H
