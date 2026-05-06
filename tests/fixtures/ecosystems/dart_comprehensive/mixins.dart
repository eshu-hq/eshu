/// Mixin patterns.

mixin Loggable {
  void log(String message) {
    print('[${runtimeType}] $message');
  }
}

mixin Serializable {
  Map<String, dynamic> toMap();

  String toJson() {
    return toMap().toString();
  }
}

class Service with Loggable {
  final String name;

  Service(this.name);

  void start() {
    log('Starting $name');
  }

  void stop() {
    log('Stopping $name');
  }
}

class TrackedService extends Service with Serializable {
  final DateTime startedAt;

  TrackedService(String name) : startedAt = DateTime.now(), super(name);

  @override
  Map<String, dynamic> toMap() {
    return {'name': name, 'startedAt': startedAt.toIso8601String()};
  }
}
