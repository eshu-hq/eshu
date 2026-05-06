#include <stdio.h>
#include <stdlib.h>
#include "types.h"

/* Function pointer typedef */
typedef void (*EventHandler)(int event_id, void* data);

/* Callback registration */
static EventHandler handlers[10];
static int handler_count = 0;

void register_handler(EventHandler handler) {
    if (handler_count < 10) {
        handlers[handler_count++] = handler;
    }
}

void dispatch_event(int event_id, void* data) {
    for (int i = 0; i < handler_count; i++) {
        handlers[i](event_id, data);
    }
}

/* Sample handlers */
void log_handler(int event_id, void* data) {
    printf("Event %d received\n", event_id);
}

void process_handler(int event_id, void* data) {
    int* value = (int*)data;
    if (value != NULL) {
        printf("Processing event %d with value %d\n", event_id, *value);
    }
}

/* Higher-order function */
int apply_transform(int value, TransformFn transform) {
    return transform(value);
}

int square(int x) {
    return x * x;
}

int negate(int x) {
    return -x;
}
