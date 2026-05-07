trait Task {
    fn run(&self) -> i32;
}

struct ReportTask;

impl Task for ReportTask {
    fn run(&self) -> i32 {
        directly_used_helper()
    }
}

fn unused_cleanup_candidate() -> i32 {
    42
}

fn directly_used_helper() -> i32 {
    7
}

pub fn public_status() -> &'static str {
    "ok"
}

fn registered_handler() -> i32 {
    3
}

fn generated_excluded_helper() -> i32 {
    13
}

const DYNAMIC_FUNCTION_NAME: &str = "registered_handler";

fn main() {
    let task: Box<dyn Task> = Box::new(ReportTask);
    let handler: fn() -> i32 = registered_handler;
    let _ = task.run() + handler();
    let _ = public_status();
}
