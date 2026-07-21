// Dogfood real-repo fixture for eshu-hq/eshu#5399.
//
// Test-shaped file, mirroring the test/ layout convention used by
// dart-lang/http-style packages. Synthetic fixture content, no external
// test framework dependency required for parsing.

import '../lib/src/http_client.dart';
import '../lib/src/models/user.dart';

void main() {
  testFetchUserReturnsDecodedUser();
  testFetchUsersReturnsList();
}

void testFetchUserReturnsDecodedUser() {
  final client = DogfoodHttpClient('https://example.invalid');
  final future = client.fetchUser('u-1');
  future.then((User user) {
    print(user.toString());
  });
}

void testFetchUsersReturnsList() {
  final client = DogfoodHttpClient('https://example.invalid');
  client.fetchUsers();
  client.close();
}
