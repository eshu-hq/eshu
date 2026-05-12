import 'package:flutter/widgets.dart';

void main() {
  directDartHelper();
  selectedDartHandler();
}

void unusedDartHelper() {}

void directDartHelper() {}

class PublicDartWidget extends StatefulWidget {
  const PublicDartWidget();

  @override
  State<PublicDartWidget> createState() => _PublicDartWidgetState();

  void render() {}
}

class _PublicDartWidgetState extends State<PublicDartWidget> {
  @override
  Widget build(BuildContext context) => const SizedBox();

  void unusedStateHelper() {}
}

final void Function() selectedDartHandler = directDartHelper;

// Generated-style fixture symbol excluded by default policy.
void generatedDartStub() {}

void dynamicDartDispatch(String name) {
  final calls = <String, void Function()>{
    'direct': directDartHelper,
  };
  calls[name]?.call();
}
