#include "fixture.hpp"

#include <functional>
#include <iostream>
#include <string>

int unusedCleanupCandidate() {
    return 42;
}

int directlyUsedHelper() {
    return 7;
}

class PublicWidget {
public:
    int render() const {
        return directlyUsedHelper();
    }
};

class Command {
public:
    virtual int run() const {
        return 3;
    }
};

class DerivedCommand : public Command {
public:
    int run() const override {
        return 5;
    }
};

class CallbackRunner {
public:
    int invoke(const std::function<int()> &callback) const {
        return callback();
    }
};

int dispatchTarget() {
    return 17;
}

int generatedExcludedHelper() {
    return 13;
}

const std::string dynamicMethodName = "run";

int eshuCppPublicAPI() {
    return 19;
}

int HeaderWidget::render() const {
    return helper();
}

int HeaderWidget::helper() const {
    return 23;
}

void NAPI_MODULE_INIT() {}

int main() {
    PublicWidget widget;
    Command command;
    DerivedCommand derived;
    CallbackRunner runner;
    int (*dispatch)(void) = dispatchTarget;
    std::cout << command.run() << "\n";
    return widget.render() + derived.run() + runner.invoke(directlyUsedHelper) + dispatch();
}
