#include "types.h"

const char* status_to_string(StatusCode code) {
    switch (code) {
        case STATUS_OK: return "OK";
        case STATUS_ERROR: return "Error";
        case STATUS_NOT_FOUND: return "Not Found";
        case STATUS_TIMEOUT: return "Timeout";
        default: return "Unknown";
    }
}
