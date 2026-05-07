void main() {
  directDartHelper();
  selectedDartHandler();
  onPressedRoot();
}

void unusedDartHelper() {}

void directDartHelper() {}

class PublicDartWidget {
  void render() {}
}

void onPressedRoot() {}

final void Function() selectedDartHandler = directDartHelper;

// Generated-style fixture symbol excluded by default policy.
void generatedDartStub() {}

void dynamicDartDispatch(String name) {
  final calls = <String, void Function()>{
    'direct': directDartHelper,
  };
  calls[name]?.call();
}
