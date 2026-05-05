#include <iostream>
#include <vector>
#include <map>
#include <algorithm>
#include <string>
#include <numeric>

void vector_operations() {
    std::vector<int> numbers = {5, 3, 1, 4, 2};

    std::sort(numbers.begin(), numbers.end());

    auto it = std::find(numbers.begin(), numbers.end(), 3);

    int sum = std::accumulate(numbers.begin(), numbers.end(), 0);

    std::vector<int> doubled;
    std::transform(numbers.begin(), numbers.end(),
                   std::back_inserter(doubled),
                   [](int n) { return n * 2; });
}

void map_operations() {
    std::map<std::string, int> scores;
    scores["alice"] = 95;
    scores["bob"] = 87;
    scores.insert({"charlie", 92});

    for (const auto& [name, score] : scores) {
        std::cout << name << ": " << score << std::endl;
    }
}

void algorithm_examples() {
    std::vector<int> data = {1, 2, 3, 4, 5, 6, 7, 8, 9, 10};

    bool allPositive = std::all_of(data.begin(), data.end(),
                                    [](int n) { return n > 0; });

    int count = std::count_if(data.begin(), data.end(),
                               [](int n) { return n % 2 == 0; });

    auto minMax = std::minmax_element(data.begin(), data.end());
}
