#ifndef TEMPLATES_H
#define TEMPLATES_H

#include <vector>
#include <algorithm>
#include <functional>

// Template function
template<typename T>
T max_value(T a, T b) {
    return (a > b) ? a : b;
}

// Template function with multiple params
template<typename T, typename U>
auto add(T a, U b) -> decltype(a + b) {
    return a + b;
}

// Template class
template<typename T>
class Stack {
public:
    void push(const T& item) {
        items_.push_back(item);
    }

    T pop() {
        T item = items_.back();
        items_.pop_back();
        return item;
    }

    bool empty() const {
        return items_.empty();
    }

    size_t size() const {
        return items_.size();
    }

private:
    std::vector<T> items_;
};

// Template specialization
template<>
class Stack<bool> {
public:
    void push(bool item) {
        bits_.push_back(item);
    }

    bool pop() {
        bool item = bits_.back();
        bits_.pop_back();
        return item;
    }

private:
    std::vector<bool> bits_;
};

// Template with type constraint (C++20)
template<typename T>
concept Numeric = std::is_arithmetic_v<T>;

template<Numeric T>
T multiply(T a, T b) {
    return a * b;
}

#endif // TEMPLATES_H
