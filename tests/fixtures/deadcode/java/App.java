package deadcode.fixture;

interface Task {
    void run();
}

public final class App {
    static final String dynamicMethodName = "run";

    private static void unusedCleanupCandidate() {
        System.out.println("unused");
    }

    private static void directlyUsedHelper() {
        System.out.println("used");
    }

    public static void main(String[] args) {
        Task task = App::directlyUsedHelper;
        task.run();
        new PublicService().status();
        new JobController().handle();
    }
}

final class PublicService {
    public String status() {
        return "ok";
    }
}

final class JobController {
    @Deprecated
    public void handle() {
        System.out.println("framework root");
    }
}

final class GeneratedExcludedHelper {
    static void render() {
        System.out.println("generated");
    }
}
