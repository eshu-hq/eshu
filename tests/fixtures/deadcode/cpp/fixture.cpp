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

class CallbackRunner {
public:
    int invoke(const std::function<int()> &callback) const {
        return callback();
    }
};

int generatedExcludedHelper() {
    return 13;
}

const std::string dynamicMethodName = "run";

int main() {
    PublicWidget widget;
    Command command;
    CallbackRunner runner;
    std::cout << command.run() << "\n";
    return widget.render() + runner.invoke(directlyUsedHelper);
}
