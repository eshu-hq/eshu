#include <stdio.h>
#include <stdlib.h>
#include "math/vector.h"
#include "types.h"

int main(int argc, char* argv[]) {
    Vector v1 = vector_create(1.0, 2.0, 3.0);
    Vector v2 = vector_create(4.0, 5.0, 6.0);

    Vector sum = vector_add(&v1, &v2);
    printf("Sum: (%f, %f, %f)\n", sum.x, sum.y, sum.z);

    double dot = vector_dot(&v1, &v2);
    printf("Dot product: %f\n", dot);

    StatusCode code = STATUS_OK;
    printf("Status: %s\n", status_to_string(code));

    return 0;
}

void process_items(int* items, int count, int (*transform)(int)) {
    for (int i = 0; i < count; i++) {
        items[i] = transform(items[i]);
    }
}

int double_value(int x) {
    return x * 2;
}
