#include <signal.h>
#include <stdio.h>
#include "fixture.h"

int unused_cleanup_candidate(void) {
    return 42;
}

static int directly_used_helper(void) {
    return 7;
}

int eshu_c_public_api(void) {
    return directly_used_helper();
}

static void registered_signal_handler(int signal_number) {
    printf("signal %d\n", signal_number);
}

static int dispatch_target(void) {
    return 11;
}

static int generated_excluded_helper(void) {
    return 13;
}

const char *dynamic_lookup_name = "dispatch_target";

int main(void) {
    int (*handler)(void) = dispatch_target;
    signal(SIGTERM, registered_signal_handler);
    return handler() + eshu_c_public_api();
}
