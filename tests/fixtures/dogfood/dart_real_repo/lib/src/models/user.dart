// Dogfood real-repo fixture for eshu-hq/eshu#5399.
//
// Shape informed by public patterns in `dart-lang/http` (client/response
// modeling) and `flutter/flutter` (public library layering under lib/src).
// This file is synthetic, hand-authored for this fixture, not copied from
// either project; the repo names are provenance metadata only.

class User {
  final String id;
  final String name;
  final String email;

  User({required this.id, required this.name, required this.email});

  factory User.fromJson(Map<String, dynamic> json) {
    return User(
      id: json['id'] as String,
      name: json['name'] as String,
      email: json['email'] as String,
    );
  }

  Map<String, dynamic> toJson() {
    return <String, dynamic>{'id': id, 'name': name, 'email': email};
  }

  @override
  String toString() => 'User($id, $name, $email)';
}

class UserNotFoundException implements Exception {
  final String userId;

  UserNotFoundException(this.userId);

  @override
  String toString() => 'UserNotFoundException: $userId';
}
