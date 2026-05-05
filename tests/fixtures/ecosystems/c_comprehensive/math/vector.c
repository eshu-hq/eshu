#include "vector.h"
#include <math.h>

Vector vector_create(double x, double y, double z) {
    Vector v = {x, y, z};
    return v;
}

Vector vector_add(const Vector* a, const Vector* b) {
    return vector_create(a->x + b->x, a->y + b->y, a->z + b->z);
}

Vector vector_sub(const Vector* a, const Vector* b) {
    return vector_create(a->x - b->x, a->y - b->y, a->z - b->z);
}

double vector_dot(const Vector* a, const Vector* b) {
    return a->x * b->x + a->y * b->y + a->z * b->z;
}

Vector vector_cross(const Vector* a, const Vector* b) {
    return vector_create(
        a->y * b->z - a->z * b->y,
        a->z * b->x - a->x * b->z,
        a->x * b->y - a->y * b->x
    );
}

double vector_magnitude(const Vector* v) {
    return sqrt(vector_dot(v, v));
}

Vector vector_normalize(const Vector* v) {
    double mag = vector_magnitude(v);
    if (mag == 0.0) return vector_create(0, 0, 0);
    return vector_scale(v, 1.0 / mag);
}

Vector vector_scale(const Vector* v, double scalar) {
    return vector_create(v->x * scalar, v->y * scalar, v->z * scalar);
}
