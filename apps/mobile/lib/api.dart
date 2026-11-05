import 'dart:async';
import 'dart:convert';
import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:geolocator/geolocator.dart';
import 'package:http/http.dart' as http;
import 'package:uuid/uuid.dart';

final apiProvider = Provider<Api>((ref) {
  final api = Api();
  ref.onDispose(api.dispose);
  return api;
});
final localeProvider = StateProvider<String>((ref) => 'my');

class ApiException implements Exception {
  final int status;
  final String code;
  const ApiException(this.status, this.code);
  @override
  String toString() => code;
}

class Api extends ChangeNotifier {
  final http.Client client;
  final FlutterSecureStorage storage;
  final String base;
  String? access, refresh, userId;
  Future<bool>? _refreshing;
  Api({http.Client? client, FlutterSecureStorage? storage, String? base})
    : client = client ?? http.Client(),
      storage = storage ?? const FlutterSecureStorage(),
      base =
          base ??
          const String.fromEnvironment(
            'API_BASE',
            defaultValue: 'http://localhost:8080',
          );
  bool get signedIn => access != null;
  void profileUpdated() => notifyListeners();
  Future<void> restore() async {
    // Web preview deliberately does not persist bearer credentials.
    if (kIsWeb) return;
    refresh = await storage.read(key: 'refresh');
    userId = await storage.read(key: 'user');
    if (refresh != null) {
      try {
        await _renew();
      } on ApiException {
        await logout();
      }
    }
  }

  Future<void> accept(Map<String, dynamic> data) async {
    access = data['access_token'] as String;
    refresh = data['refresh_token'] as String;
    userId = data['user_id'] as String? ?? userId;
    if (!kIsWeb) {
      await storage.write(key: 'refresh', value: refresh);
      await storage.write(key: 'user', value: userId);
    }
    notifyListeners();
  }

  Future<bool> _renew() async {
    final old = refresh;
    if (old == null) return false;
    final data = await request(
      'POST',
      '/v1/auth/refresh',
      body: {'refresh_token': old},
      retry: false,
    );
    await accept(data);
    return true;
  }

  Future<Map<String, dynamic>> request(
    String method,
    String path, {
    Map<String, dynamic>? body,
    bool retry = true,
  }) async {
    final uri = Uri.parse('$base$path');
    if (!kDebugMode && uri.scheme != 'https') {
      throw const ApiException(0, 'https_required');
    }
    final req = http.Request(method, uri);
    req.headers['Content-Type'] = 'application/json';
    if (access != null) req.headers['Authorization'] = 'Bearer $access';
    if (body != null) req.body = jsonEncode(body);
    final res = await http.Response.fromStream(
      await client.send(req).timeout(const Duration(seconds: 15)),
    );
    if (res.statusCode == 401 && retry && refresh != null) {
      _refreshing ??= _renew();
      bool ok;
      try {
        ok = await _refreshing!;
      } finally {
        _refreshing = null;
      }
      if (ok) return request(method, path, body: body, retry: false);
    }
    final data = res.body.isEmpty
        ? <String, dynamic>{}
        : jsonDecode(res.body) as Map<String, dynamic>;
    if (res.statusCode >= 400) {
      throw ApiException(
        res.statusCode,
        data['error'] as String? ?? 'request_failed',
      );
    }
    return data;
  }

  Future<void> updateLocation(String township) async {
    if (!await Geolocator.isLocationServiceEnabled()) {
      throw const ApiException(0, 'location_disabled');
    }
    var p = await Geolocator.checkPermission();
    if (p == LocationPermission.denied) {
      p = await Geolocator.requestPermission();
    }
    if (p == LocationPermission.denied ||
        p == LocationPermission.deniedForever) {
      throw const ApiException(0, 'location_denied');
    }
    final fix = await Geolocator.getCurrentPosition(
      locationSettings: const LocationSettings(
        accuracy: LocationAccuracy.high,
        timeLimit: Duration(seconds: 20),
      ),
    );
    if (fix.accuracy > 100) {
      throw const ApiException(0, 'location_accuracy_too_low');
    }
    await request(
      'POST',
      '/v1/location',
      body: {
        'lat': fix.latitude,
        'lng': fix.longitude,
        'accuracy': fix.accuracy.round(),
        'township': township,
        'nonce': const Uuid().v4(),
      },
    );
  }

  Future<void> logout() async {
    access = null;
    refresh = null;
    userId = null;
    if (!kIsWeb) {
      await storage.delete(key: 'refresh');
      await storage.delete(key: 'user');
    }
    notifyListeners();
  }

  @override
  void dispose() {
    client.close();
    super.dispose();
  }
}
