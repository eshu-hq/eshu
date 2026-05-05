package comprehensive.annotations;

/**
 * Service using custom annotation.
 */
@Logged("service")
public class AnnotatedService {

    @Logged(value = "process", includeArgs = true)
    public String process(String input) {
        return input.toUpperCase();
    }

    @Logged
    public void cleanup() {
        // cleanup logic
    }
}
