package comprehensive;

import java.util.List;
import java.util.Arrays;

/**
 * Main entry point.
 */
public class Main {
    public static void main(String[] args) {
        Person person = new Person("Alice", 30);
        Employee employee = new Employee("Bob", 25, "Engineering");

        System.out.println(person.greet());
        System.out.println(employee.greet());

        Greetable greetable = person;
        System.out.println(greetable.getGreeting());

        Container<String> container = new Container<>();
        container.add("hello");
        System.out.println(container.get(0));
    }

    public static <T extends Comparable<T>> T max(T a, T b) {
        return a.compareTo(b) > 0 ? a : b;
    }
}
