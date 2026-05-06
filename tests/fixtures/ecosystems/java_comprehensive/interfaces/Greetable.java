package comprehensive.interfaces;

/**
 * Interface with default method.
 */
public interface Greetable {
    String getGreeting();

    default String greetLoudly() {
        return getGreeting().toUpperCase();
    }

    static String defaultGreeting() {
        return "Hello, World!";
    }
}
