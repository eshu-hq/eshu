// Dogfood real-repo fixture for eshu-hq/eshu#5399.
//
// Shape informed by public patterns in `dart-lang/http` (a thin client
// wrapper over request/response handling). Synthetic, hand-authored for
// this fixture; the repo name is provenance metadata only, never a
// fetched dependency.

import 'dart:convert';
import 'dart:async';

import 'models/user.dart';

class DogfoodHttpClient {
  final String baseUrl;
  final Map<String, String> defaultHeaders;

  DogfoodHttpClient(this.baseUrl, {this.defaultHeaders = const {}});

  Future<User> fetchUser(String userId) async {
    final response = await _get('/users/$userId');
    if (response.statusCode == 404) {
      throw UserNotFoundException(userId);
    }
    final decoded = jsonDecode(response.body) as Map<String, dynamic>;
    return User.fromJson(decoded);
  }

  Future<List<User>> fetchUsers() async {
    final response = await _get('/users');
    final decoded = jsonDecode(response.body) as List<dynamic>;
    return decoded
        .map((item) => User.fromJson(item as Map<String, dynamic>))
        .toList();
  }

  Future<DogfoodResponse> _get(String path) async {
    return DogfoodResponse(200, '{}');
  }

  void close() {
    // No persistent connection to release in this synthetic fixture.
  }
}

class DogfoodResponse {
  final int statusCode;
  final String body;

  DogfoodResponse(this.statusCode, this.body);
}
