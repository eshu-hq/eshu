package comprehensive.generics;

import java.util.ArrayList;
import java.util.List;
import java.util.function.Predicate;

/**
 * Generic container class.
 */
public class Container<T> {
    private final List<T> items = new ArrayList<>();

    public void add(T item) {
        items.add(item);
    }

    public T get(int index) {
        return items.get(index);
    }

    public List<T> filter(Predicate<T> predicate) {
        List<T> result = new ArrayList<>();
        for (T item : items) {
            if (predicate.test(item)) {
                result.add(item);
            }
        }
        return result;
    }

    public <U> Container<U> map(java.util.function.Function<T, U> mapper) {
        Container<U> result = new Container<>();
        for (T item : items) {
            result.add(mapper.apply(item));
        }
        return result;
    }

    public int size() {
        return items.size();
    }
}
