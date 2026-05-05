#include <memory>
#include <iostream>
#include <string>
#include <vector>

class Resource {
public:
    Resource(const std::string& name) : name_(name) {
        std::cout << "Created: " << name_ << std::endl;
    }
    ~Resource() {
        std::cout << "Destroyed: " << name_ << std::endl;
    }
    void use() const { std::cout << "Using: " << name_ << std::endl; }
    const std::string& getName() const { return name_; }

private:
    std::string name_;
};

void unique_ptr_examples() {
    // Create unique_ptr
    auto resource = std::make_unique<Resource>("unique");
    resource->use();

    // Transfer ownership
    auto moved = std::move(resource);
    moved->use();

    // unique_ptr in container
    std::vector<std::unique_ptr<Resource>> resources;
    resources.push_back(std::make_unique<Resource>("vec-item"));
}

void shared_ptr_examples() {
    // Create shared_ptr
    auto shared = std::make_shared<Resource>("shared");
    shared->use();

    // Copy shared_ptr
    auto copy = shared;
    std::cout << "Use count: " << shared.use_count() << std::endl;

    // Method call through pointer
    copy->use();
    copy->getName();
}

void weak_ptr_examples() {
    std::weak_ptr<Resource> weak;
    {
        auto shared = std::make_shared<Resource>("weak-test");
        weak = shared;

        if (auto locked = weak.lock()) {
            locked->use();
        }
    }
    // shared is destroyed, weak is expired
    std::cout << "Expired: " << weak.expired() << std::endl;
}
