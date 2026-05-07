protocol Task {
    func run() -> Int
}

struct ReportTask: Task {
    func run() -> Int {
        return directlyUsedHelper()
    }
}

func unusedCleanupCandidate() -> Int {
    return 42
}

func directlyUsedHelper() -> Int {
    return 7
}

public struct PublicService {
    public func status() -> String {
        return "ok"
    }
}

class ViewController {
    func viewDidLoad() {
        _ = PublicService().status()
    }
}

func generatedExcludedHelper() -> Int {
    return 13
}

let dynamicMethodName = "run"

@main
struct AppMain {
    static func main() {
        let task: Task = ReportTask()
        _ = task.run()
        ViewController().viewDidLoad()
    }
}
