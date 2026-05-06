/// Async patterns.

import 'dart:async';

Future<String> fetchData(String url) async {
  await Future.delayed(Duration(milliseconds: 100));
  return 'data from $url';
}

Stream<int> numberStream(int count) async* {
  for (var i = 0; i < count; i++) {
    await Future.delayed(Duration(milliseconds: 50));
    yield i;
  }
}

Future<List<String>> fetchAll(List<String> urls) async {
  return Future.wait(urls.map(fetchData));
}

class AsyncWorker {
  final StreamController<String> _controller = StreamController<String>();

  Stream<String> get events => _controller.stream;

  Future<void> process(String item) async {
    final result = await fetchData(item);
    _controller.add(result);
  }

  void dispose() {
    _controller.close();
  }
}
