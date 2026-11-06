import 'dart:convert';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:nexus_mobile/api.dart';

const storageChannel = MethodChannel(
  'plugins.it_nomads.com/flutter_secure_storage',
);
const geoChannel = MethodChannel('flutter.baseflow.com/geolocator');

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  final storage = <String, String>{};
  var serviceEnabled = true;
  var permission = 2;
  var accuracy = 8.0;
  var requests = <http.Request>[];

  void mockStorage() {
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(storageChannel, (call) async {
          final args = (call.arguments as Map).cast<String, dynamic>();
          final key = args['key'] as String?;
          switch (call.method) {
            case 'write':
              storage[key!] = args['value'] as String;
            case 'read':
              return storage[key];
            case 'delete':
              storage.remove(key);
          }
          return null;
        });
  }

  void mockGeo() {
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(geoChannel, (call) async {
          switch (call.method) {
            case 'isLocationServiceEnabled':
              return serviceEnabled;
            case 'checkPermission':
            case 'requestPermission':
              return permission;
            case 'getCurrentPosition':
              return {
                'latitude': 16.8,
                'longitude': 96.15,
                'timestamp': DateTime.now().millisecondsSinceEpoch,
                'accuracy': accuracy,
                'altitude': 0,
                'speed': 0,
              };
          }
          return null;
        });
  }

  Api build(MockClient client) => Api(client: client, base: 'http://test');

  setUp(() {
    storage.clear();
    serviceEnabled = true;
    permission = 2;
    accuracy = 8;
    requests = [];
    mockStorage();
    mockGeo();
  });

  tearDown(() {
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(storageChannel, null);
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(geoChannel, null);
  });

  MockClient responding(Future<http.Response> Function(http.Request) handle) =>
      MockClient((req) {
        requests.add(req);
        return handle(req);
      });

  test('empty body returns empty map', () async {
    final api = build(responding((_) async => http.Response('', 204)));
    expect(await api.request('GET', '/v1/ping'), isEmpty);
  });

  test('request sends bearer token and JSON body', () async {
    final api = build(
      responding((_) async => http.Response(jsonEncode({'ok': true}), 200)),
    );
    api.access = 'token';
    final d = await api.request('POST', '/v1/x', body: {'a': 1});
    expect(d['ok'], true);
    expect(requests.single.headers['Authorization'], 'Bearer token');
    expect(requests.single.headers['Content-Type'], 'application/json');
    expect(jsonDecode(requests.single.body), {'a': 1});
  });

  test('server error exposes the error code', () async {
    final api = build(
      responding(
        (_) async => http.Response(jsonEncode({'error': 'nope'}), 422),
      ),
    );
    await expectLater(
      api.request('GET', '/v1/x'),
      throwsA(
        isA<ApiException>()
            .having((e) => e.status, 'status', 422)
            .having((e) => e.code, 'code', 'nope'),
      ),
    );
  });

  test('server error without code falls back', () async {
    final api = build(responding((_) async => http.Response('{}', 500)));
    await expectLater(
      api.request('GET', '/v1/x'),
      throwsA(
        isA<ApiException>().having((e) => e.code, 'code', 'request_failed'),
      ),
    );
  });

  test('401 refreshes once and retries', () async {
    var calls = 0;
    final api = build(
      responding((req) async {
        if (req.url.path == '/v1/auth/refresh') {
          return http.Response(
            jsonEncode({'access_token': 'a2', 'refresh_token': 'r2'}),
            200,
          );
        }
        calls++;
        return calls == 1
            ? http.Response('{}', 401)
            : http.Response(jsonEncode({'ok': true}), 200);
      }),
    );
    api.access = 'a1';
    api.refresh = 'r1';
    expect((await api.request('GET', '/v1/x'))['ok'], true);
    expect(api.access, 'a2');
    expect(storage['refresh'], 'r2');
    expect(calls, 2);
  });

  test('failed refresh surfaces the refresh error', () async {
    final api = build(responding((_) async => http.Response('{}', 401)));
    api.access = 'a1';
    api.refresh = 'r1';
    await expectLater(
      api.request('GET', '/v1/x'),
      throwsA(isA<ApiException>()),
    );
  });

  test('accept stores tokens and notifies', () async {
    final api = build(responding((_) async => http.Response('{}', 200)));
    var notified = 0;
    api.addListener(() => notified++);
    await api.accept({
      'access_token': 'a',
      'refresh_token': 'r',
      'user_id': 'u',
    });
    expect(api.signedIn, true);
    expect(api.userId, 'u');
    expect(storage['user'], 'u');
    expect(notified, 1);
    api.profileUpdated();
    expect(notified, 2);
  });

  test('restore renews a stored session', () async {
    storage['refresh'] = 'r';
    storage['user'] = 'u0';
    final api = build(
      responding(
        (_) async => http.Response(
          jsonEncode({'access_token': 'a', 'refresh_token': 'r2'}),
          200,
        ),
      ),
    );
    await api.restore();
    expect(api.signedIn, true);
    expect(api.access, 'a');
  });

  test('restore signs out when renewal is rejected', () async {
    storage['refresh'] = 'stale';
    final api = build(responding((_) async => http.Response('{}', 401)));
    await api.restore();
    expect(api.signedIn, false);
    expect(storage.containsKey('refresh'), false);
  });

  test('restore without stored refresh stays signed out', () async {
    final api = build(responding((_) async => http.Response('{}', 200)));
    await api.restore();
    expect(api.signedIn, false);
    expect(requests, isEmpty);
  });

  test('logout clears credentials and storage', () async {
    final api = build(responding((_) async => http.Response('{}', 200)));
    await api.accept({
      'access_token': 'a',
      'refresh_token': 'r',
      'user_id': 'u',
    });
    await api.logout();
    expect(api.signedIn, false);
    expect(api.userId, null);
    expect(storage, isEmpty);
  });

  test('updateLocation posts a private fix', () async {
    final api = build(
      responding((_) async => http.Response(jsonEncode({'ok': true}), 200)),
    );
    await api.updateLocation('kamayut');
    final body = jsonDecode(requests.single.body) as Map<String, dynamic>;
    expect(body['township'], 'kamayut');
    expect(body['accuracy'], 8);
    expect((body['nonce'] as String).length, 36);
  });

  test('updateLocation requests permission when denied once', () async {
    permission = 0;
    final api = build(
      responding((_) async => http.Response(jsonEncode({'ok': true}), 200)),
    );
    await expectLater(
      api.updateLocation('bahan'),
      throwsA(
        isA<ApiException>().having((e) => e.code, 'code', 'location_denied'),
      ),
    );
  });

  test('updateLocation rejects a disabled service', () async {
    serviceEnabled = false;
    final api = build(responding((_) async => http.Response('{}', 200)));
    await expectLater(
      api.updateLocation('bahan'),
      throwsA(
        isA<ApiException>().having((e) => e.code, 'code', 'location_disabled'),
      ),
    );
  });

  test('updateLocation rejects low accuracy', () async {
    accuracy = 900;
    final api = build(responding((_) async => http.Response('{}', 200)));
    await expectLater(
      api.updateLocation('bahan'),
      throwsA(
        isA<ApiException>().having(
          (e) => e.code,
          'code',
          'location_accuracy_too_low',
        ),
      ),
    );
  });

  test('dispose closes the client', () {
    final client = _ClosableClient();
    Api(client: client, base: 'http://test').dispose();
    expect(client.closed, true);
  });
}

class _ClosableClient extends http.BaseClient {
  bool closed = false;
  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) async =>
      http.StreamedResponse(Stream.value(utf8.encode('{}')), 200);
  @override
  void close() {
    closed = true;
  }
}
