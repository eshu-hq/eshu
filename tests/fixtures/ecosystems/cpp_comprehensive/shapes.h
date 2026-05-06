#ifndef SHAPES_H
#define SHAPES_H

#include <string>
#include <cmath>

/**
 * Abstract base class.
 */
class Shape {
public:
    virtual ~Shape() = default;
    virtual double area() const = 0;
    virtual double perimeter() const = 0;
    virtual std::string describe() const;
};

class Circle : public Shape {
public:
    explicit Circle(double radius);
    double area() const override;
    double perimeter() const override;
    std::string describe() const override;
    double getRadius() const { return radius_; }

private:
    double radius_;
};

class Rectangle : public Shape {
public:
    Rectangle(double width, double height);
    double area() const override;
    double perimeter() const override;
    std::string describe() const override;

protected:
    double width_;
    double height_;
};

class Square : public Rectangle {
public:
    explicit Square(double side);
    std::string describe() const override;
};

#endif // SHAPES_H
