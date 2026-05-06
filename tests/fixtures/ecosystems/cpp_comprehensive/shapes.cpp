#include "shapes.h"
#include <sstream>

std::string Shape::describe() const {
    return "Shape";
}

Circle::Circle(double radius) : radius_(radius) {}

double Circle::area() const {
    return M_PI * radius_ * radius_;
}

double Circle::perimeter() const {
    return 2 * M_PI * radius_;
}

std::string Circle::describe() const {
    std::ostringstream oss;
    oss << "Circle(r=" << radius_ << ")";
    return oss.str();
}

Rectangle::Rectangle(double width, double height)
    : width_(width), height_(height) {}

double Rectangle::area() const {
    return width_ * height_;
}

double Rectangle::perimeter() const {
    return 2 * (width_ + height_);
}

std::string Rectangle::describe() const {
    std::ostringstream oss;
    oss << "Rectangle(" << width_ << "x" << height_ << ")";
    return oss.str();
}

Square::Square(double side) : Rectangle(side, side) {}

std::string Square::describe() const {
    std::ostringstream oss;
    oss << "Square(" << width_ << ")";
    return oss.str();
}
