#ifndef VECTOR_H
#define VECTOR_H

typedef struct {
    double x;
    double y;
    double z;
} Vector;

/* Create a new vector */
Vector vector_create(double x, double y, double z);

/* Add two vectors */
Vector vector_add(const Vector* a, const Vector* b);

/* Subtract two vectors */
Vector vector_sub(const Vector* a, const Vector* b);

/* Dot product */
double vector_dot(const Vector* a, const Vector* b);

/* Cross product */
Vector vector_cross(const Vector* a, const Vector* b);

/* Magnitude */
double vector_magnitude(const Vector* v);

/* Normalize */
Vector vector_normalize(const Vector* v);

/* Scale */
Vector vector_scale(const Vector* v, double scalar);

#endif /* VECTOR_H */
