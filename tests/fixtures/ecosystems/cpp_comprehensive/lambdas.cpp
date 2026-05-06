#include <iostream>
#include <vector>
#include <functional>
#include <algorithm>

void lambda_basics() {
    // Simple lambda
    auto greet = []() { std::cout << "Hello" << std::endl; };
    greet();

    // Lambda with parameters
    auto add = [](int a, int b) { return a + b; };
    int sum = add(3, 4);

    // Lambda with capture
    int factor = 3;
    auto multiply = [factor](int x) { return x * factor; };

    // Mutable capture
    int counter = 0;
    auto increment = [&counter]() mutable { return ++counter; };

    // Capture all by reference
    auto printAll = [&]() {
        std::cout << factor << " " << counter << std::endl;
    };
}

void lambda_with_stl() {
    std::vector<int> numbers = {1, 2, 3, 4, 5};

    std::for_each(numbers.begin(), numbers.end(),
                  [](int n) { std::cout << n << " "; });

    std::sort(numbers.begin(), numbers.end(),
              [](int a, int b) { return a > b; });
}

// std::function usage
using Callback = std::function<void(const std::string&)>;

void registerCallback(Callback cb) {
    cb("event occurred");
}

// Generic lambda (C++14+)
auto genericAdd = [](auto a, auto b) { return a + b; };
