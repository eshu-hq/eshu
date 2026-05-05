package comprehensive.classes;

import comprehensive.interfaces.Greetable;

/**
 * Employee extends Person.
 */
public class Employee extends Person {
    private final String department;

    public Employee(String name, int age, String department) {
        super(name, age);
        this.department = department;
    }

    public String getDepartment() {
        return department;
    }

    @Override
    public String getGreeting() {
        return String.format("Hi, I'm %s from %s", getName(), department);
    }

    @Override
    public String toString() {
        return String.format("Employee{name='%s', department='%s'}", getName(), department);
    }
}
