#include <stdlib.h>
#include "orders.h"

struct order *order_create(const char *customer_id) {
    struct order *o = malloc(sizeof(struct order));
    o->customer_id = customer_id;
    o->line_count = 0;
    return o;
}

int order_add_line(struct order *o, const char *sku) {
    if (o == NULL) {
        return -1;
    }
    o->line_count++;
    return o->line_count;
}
