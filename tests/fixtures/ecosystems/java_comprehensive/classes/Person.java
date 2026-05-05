package comprehensive.classes;

import comprehensive.interfaces.Greetable;

/**
 * Base person class.
 */
public class Person implements Greetable {
    private final String name;
    private final int age;

    public Person(String name, int age) {
        this.name = name;
        this.age = age;
    }

    public String getName() {
        return name;
    }

    public int getAge() {
        return age;
    }

    @Override
    public String getGreeting() {
        return String.format("Hi, I'm %s", name);
    }

    public String greet() {
        return getGreeting();
    }

    @Override
    public String toString() {
        return String.format("Person{name='%s', age=%d}", name, age);
    }
}
