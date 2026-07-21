// Dogfood real-repo fixture for eshu-hq/eshu#5399.
//
// Public library entrypoint (outside lib/src) exporting the package's API
// surface, mirroring how `dart-lang/http`-shaped packages expose a single
// lib/<package>.dart barrel file. Synthetic fixture content.

library dogfood_http;

export 'src/http_client.dart';
export 'src/models/user.dart';

import 'src/http_client.dart';

DogfoodHttpClient createDefaultClient(String baseUrl) {
  return DogfoodHttpClient(baseUrl, defaultHeaders: {'Accept': 'application/json'});
}
