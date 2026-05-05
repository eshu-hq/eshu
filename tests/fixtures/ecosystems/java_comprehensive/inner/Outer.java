package comprehensive.inner;

/**
 * Inner classes, static nested, and anonymous.
 */
public class Outer {
    private int value = 10;

    public class Inner {
        public int getValue() {
            return value;
        }
    }

    public static class StaticNested {
        public String describe() {
            return "I'm statically nested";
        }
    }

    public interface Callback {
        void onComplete(String result);
    }

    public void doWork(Callback callback) {
        // Anonymous class
        Callback logger = new Callback() {
            @Override
            public void onComplete(String result) {
                System.out.println("Logged: " + result);
            }
        };
        logger.onComplete("work done");
        callback.onComplete("work done");
    }

    public Runnable createTask() {
        // Lambda implementing interface
        return () -> System.out.println("Task running with value: " + value);
    }
}
