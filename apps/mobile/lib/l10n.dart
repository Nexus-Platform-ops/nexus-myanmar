import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

class Strings {
  final Map<String, dynamic> values;
  Strings(this.values);
  String get(String key) => values[key]?.toString() ?? key;
  static Strings of(BuildContext c) => Localizations.of<Strings>(c, Strings)!;
  static const delegate = _Delegate();
}

class _Delegate extends LocalizationsDelegate<Strings> {
  const _Delegate();
  @override
  bool isSupported(Locale l) => ['en', 'my', 'zh'].contains(l.languageCode);
  @override
  Future<Strings> load(Locale l) async => Strings(
    jsonDecode(
          await rootBundle.loadString('assets/i18n/${l.languageCode}.json'),
        )
        as Map<String, dynamic>,
  );
  @override
  bool shouldReload(_Delegate old) => false;
}

extension Copy on BuildContext {
  String tr(String key) => Strings.of(this).get(key);
}
