#include <string>
#include <vector>

namespace orders {

class OrderService {
public:
    explicit OrderService(int capacity) : capacity_(capacity) {}

    int place(const std::string &customer_id, const std::vector<std::string> &skus) {
        int lines = 0;
        for (const auto &sku : skus) {
            lines += addLine(sku);
        }
        return lines;
    }

private:
    int addLine(const std::string &sku) { return capacity_ > 0 ? 1 : 0; }
    int capacity_;
};

} // namespace orders
