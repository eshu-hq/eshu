#ifndef TYPES_H
#define TYPES_H

/* Typedef for status codes */
typedef enum {
    STATUS_OK = 0,
    STATUS_ERROR = 1,
    STATUS_NOT_FOUND = 2,
    STATUS_TIMEOUT = 3
} StatusCode;

/* Typedef for a callback function */
typedef int (*TransformFn)(int);
typedef void (*CallbackFn)(const char* message);

/* Union for generic value */
typedef union {
    int intVal;
    float floatVal;
    char strVal[64];
} GenericValue;

/* Struct with union */
typedef struct {
    int type;
    GenericValue value;
} TypedValue;

/* Macro definitions */
#define MAX_BUFFER_SIZE 4096
#define ARRAY_SIZE(arr) (sizeof(arr) / sizeof((arr)[0]))
#define CLAMP(x, lo, hi) ((x) < (lo) ? (lo) : ((x) > (hi) ? (hi) : (x)))

/* Function prototype */
const char* status_to_string(StatusCode code);

#endif /* TYPES_H */
