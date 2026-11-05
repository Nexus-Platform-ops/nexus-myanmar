import 'dart:convert';
import 'dart:io';
import 'dart:ui' as ui;
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_localizations/flutter_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:nexus_mobile/api.dart';
import 'package:nexus_mobile/l10n.dart';
import 'package:nexus_mobile/main.dart';
import 'package:nexus_mobile/community.dart';
import 'package:nexus_mobile/discover.dart';
import 'package:nexus_mobile/ui.dart';

class FakeApi extends Api {
  bool failReaction = false;
  FakeApi() : super(client: MockClient((_) async => http.Response('{}', 200)));
  @override
  Future<void> restore() async {}
  @override
  Future<Map<String, dynamic>> request(
    String method,
    String path, {
    Map<String, dynamic>? body,
    bool retry = true,
  }) async {
    if (failReaction && path.endsWith('/reactions')) {
      throw const ApiException(503, 'unavailable');
    }
    if (path == '/v1/profile') {
      return {
        'real_name': 'Social Person',
        'bio': 'Hello',
        'township': 'kamayut',
        'ghost': false,
        'tier': 1,
      };
    }
    return {'items': <dynamic>[], 'quiet': true};
  }
}

Future<void> mount(
  WidgetTester tester,
  Widget child, {
  String locale = 'en',
  FakeApi? api,
  Size size = const Size(390, 844),
}) async {
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);
  await tester.pumpWidget(
    ProviderScope(
      overrides: [apiProvider.overrideWithValue(api ?? FakeApi())],
      child: MaterialApp(
        locale: Locale(locale),
        supportedLocales: const [Locale('en'), Locale('my'), Locale('zh')],
        localizationsDelegates: const [
          Strings.delegate,
          GlobalMaterialLocalizations.delegate,
          GlobalWidgetsLocalizations.delegate,
          GlobalCupertinoLocalizations.delegate,
        ],
        theme: ThemeData(
          fontFamily: 'NotoSansMyanmar',
          colorScheme: ColorScheme.fromSeed(seedColor: primary),
        ),
        builder: (context, child) => MediaQuery(
          data: MediaQuery.of(context).copyWith(disableAnimations: true),
          child: child!,
        ),
        home: Scaffold(body: SafeArea(child: child)),
      ),
    ),
  );
  await tester.runAsync(() async {
    await Future<void>.delayed(const Duration(milliseconds: 150));
  });
  await tester.pumpAndSettle();
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUpAll(() async {
    final loader = FontLoader('NotoSansMyanmar')
      ..addFont(rootBundle.load('assets/fonts/NotoSansMyanmar.ttf'));
    await loader.load();
    final icons = FontLoader('MaterialIcons')
      ..addFont(rootBundle.load('fonts/MaterialIcons-Regular.otf'));
    await icons.load();
  });
  test('locale keys identical', () async {
    final en =
        jsonDecode(await rootBundle.loadString('assets/i18n/en.json')) as Map;
    for (final code in ['my', 'zh']) {
      final data =
          jsonDecode(await rootBundle.loadString('assets/i18n/$code.json'))
              as Map;
      expect(data.keys.toSet(), en.keys.toSet());
      expect(data.values.every((v) => v is String && v.isNotEmpty), true);
    }
  });
  for (final locale in ['en', 'my', 'zh']) {
    testWidgets('onboarding $locale fits phone and advances', (tester) async {
      await mount(tester, const Welcome(), locale: locale);
      expect(tester.takeException(), isNull);
      await tester.tap(find.byType(FilledButton));
      await tester.pumpAndSettle();
      expect(tester.takeException(), isNull);
    });
  }
  testWidgets('Myanmar stacked consonants render without layout exception', (
    tester,
  ) async {
    await mount(
      tester,
      const Padding(
        padding: EdgeInsets.all(16),
        child: Text(
          'မြန်မာနိုင်ငံ၏ အသိုင်းအဝိုင်းနှင့် လုံခြုံရေးစနစ်',
          style: TextStyle(fontSize: 18, height: 1.6),
        ),
      ),
      locale: 'my',
    );
    expect(tester.takeException(), isNull);
  });
  testWidgets('main navigation has four destinations and create FAB', (
    tester,
  ) async {
    await mount(tester, const Home());
    expect(find.byType(NavigationDestination), findsNWidgets(4));
    expect(find.byType(FloatingActionButton), findsOneWidget);
    await tester.tap(find.byIcon(Icons.people_outline));
    await tester.pumpAndSettle();
    expect(find.byType(Community), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
  testWidgets('quiet radar exposes no identity', (tester) async {
    await mount(tester, const Discover());
    expect(find.textContaining('Radar quiet'), findsOneWidget);
    expect(find.byType(ProfileCard), findsNothing);
  });
  testWidgets('anonymous radar blips accessible', (tester) async {
    await mount(
      tester,
      RadarView(
        blips: [
          {'display_angle': 90, 'display_ring': 1, 'distance_bucket': '1km'},
        ],
        onTap: (_) {},
      ),
    );
    expect(find.byType(Blip), findsOneWidget);
    await tester.pumpWidget(const SizedBox());
  });
  testWidgets('reaction failure rolls back selected state', (tester) async {
    final api = FakeApi()..failReaction = true;
    await mount(
      tester,
      PostCard(
        data: {
          'id': 'post',
          'author_id': 'author',
          'author': 'Social Person',
          'township': 'kamayut',
          'audience': 'PUBLIC',
          'body': 'Hello',
          'status': 'LIVE',
          'reaction_count': 0,
          'my_reaction': null,
          'comment_count': 0,
          'share_count': 0,
        },
      ),
      api: api,
    );
    await tester.tap(find.text('Hello'));
    await tester.tap(find.text('Hello'));
    await tester.pumpAndSettle();
    expect(find.textContaining('♡ 0'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
  testWidgets('English onboarding visual capture', (tester) async {
    final key = GlobalKey();
    await mount(tester, RepaintBoundary(key: key, child: const Welcome()));
    final boundary =
        key.currentContext!.findRenderObject()! as RenderRepaintBoundary;
    await tester.runAsync(() async {
      final image = await boundary.toImage(pixelRatio: 2);
      final bytes = await image.toByteData(format: ui.ImageByteFormat.png);
      if (const String.fromEnvironment('CAPTURE') == '1') {
        final f = File('../../evidence/mobile-onboarding.png');
        await f.parent.create(recursive: true);
        await f.writeAsBytes(bytes!.buffer.asUint8List());
      }
      image.dispose();
    });
  });
}
