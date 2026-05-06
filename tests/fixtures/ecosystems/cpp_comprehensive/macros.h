#ifndef MACROS_H
#define MACROS_H

// Object-like macros
#define MAX_SIZE 1024
#define VERSION "1.0.0"
#define DEBUG_MODE 1

// Function-like macros
#define MIN(a, b) ((a) < (b) ? (a) : (b))
#define MAX(a, b) ((a) > (b) ? (a) : (b))
#define SQUARE(x) ((x) * (x))

// Conditional compilation
#ifdef DEBUG_MODE
    #define LOG(msg) std::cout << "[DEBUG] " << msg << std::endl
#else
    #define LOG(msg)
#endif

// Variadic macro
#define PRINT_ARGS(...) printf(__VA_ARGS__)

// Stringify
#define STRINGIFY(x) #x
#define TOSTRING(x) STRINGIFY(x)

#endif // MACROS_H
