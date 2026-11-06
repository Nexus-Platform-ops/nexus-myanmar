import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_localizations/flutter_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:nexus_mobile/api.dart';
import 'package:nexus_mobile/chat.dart';
import 'package:nexus_mobile/community.dart';
import 'package:nexus_mobile/discover.dart';
import 'package:nexus_mobile/l10n.dart';
import 'package:nexus_mobile/main.dart';
import 'package:nexus_mobile/profile.dart';
import 'package:nexus_mobile/ui.dart';

class FakeApi extends Api {
  FakeApi() : super(client: MockClient((_) async => http.Response('{}', 200)));

  final responses = <String, Object?>{};
  final failures = <String>{};
  final calls = <String>[];
  final bodies = <String, Map<String, dynamic>>{};
  Map<String, dynamic>? lastBody;
  bool updatedProfile = false;
  bool signedOut = false;
  int locationUpdates = 0;

  @override
  Future<void> restore() async {}

  @override
  Future<void> accept(Map<String, dynamic> data) async {
    access = data['access_token'] as String?;
    refresh = data['refresh_token'] as String?;
    userId = data['user_id'] as String?;
    notifyListeners();
  }

  @override
  void profileUpdated() => updatedProfile = true;

  @override
  Future<void> updateLocation(String township) async => locationUpdates++;

  @override
  Future<void> logout() async {
    access = null;
    refresh = null;
    userId = null;
    signedOut = true;
    notifyListeners();
  }

  @override
  Future<Map<String, dynamic>> request(
    String method,
    String path, {
    Map<String, dynamic>? body,
    bool retry = true,
  }) async {
    final key = '$method $path';
    calls.add(key);
    if (body != null) {
      lastBody = body;
      bodies[key] = body;
    }
    if (failures.contains(key) || failures.contains('*')) {
      throw const ApiException(500, 'boom');
    }
    final v = responses[key] ?? responses[path];
    if (v is ApiException) throw v;
    if (v is Map) {
      return jsonDecode(jsonEncode(v)) as Map<String, dynamic>;
    }
    return {'items': <dynamic>[], 'quiet': true};
  }
}

const profilePayload = {
  'real_name': 'Social Person',
  'bio': 'Hello',
  'township': 'kamayut',
  'ghost': false,
  'tier': 1,
};

FakeApi seedApi() {
  final api = FakeApi();
  api.responses['GET /v1/profile'] = profilePayload;
  return api;
}

ThemeData theme() => ThemeData(
  useMaterial3: true,
  fontFamily: 'NotoSansMyanmar',
  colorScheme: ColorScheme.fromSeed(seedColor: primary),
);

Widget app({required Widget home, FakeApi? api, String locale = 'en'}) =>
    ProviderScope(
      overrides: [apiProvider.overrideWithValue(api ?? seedApi())],
      child: MaterialApp(
        locale: Locale(locale),
        supportedLocales: const [Locale('en'), Locale('my'), Locale('zh')],
        localizationsDelegates: const [
          Strings.delegate,
          GlobalMaterialLocalizations.delegate,
          GlobalWidgetsLocalizations.delegate,
          GlobalCupertinoLocalizations.delegate,
        ],
        theme: theme(),
        builder: (context, child) => MediaQuery(
          data: MediaQuery.of(context).copyWith(disableAnimations: true),
          child: child!,
        ),
        home: Scaffold(body: SafeArea(child: home)),
      ),
    );

Future<void> settle(WidgetTester tester) async {
  await tester.runAsync(() async {
    await Future<void>.delayed(const Duration(milliseconds: 150));
  });
  await tester.pumpAndSettle();
}

Future<void> mount(
  WidgetTester tester,
  Widget child, {
  FakeApi? api,
  String locale = 'en',
  Size size = const Size(390, 844),
}) async {
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);
  await tester.pumpWidget(app(home: child, api: api, locale: locale));
  await settle(tester);
}

Future<void> mountPushed(
  WidgetTester tester,
  Widget child, {
  FakeApi? api,
  Size size = const Size(390, 844),
}) async {
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);
  await tester.pumpWidget(
    app(
      api: api,
      home: Builder(
        builder: (c) => Center(
          child: TextButton(
            onPressed: () => open(c, child),
            child: const Text('__open__'),
          ),
        ),
      ),
    ),
  );
  await settle(tester);
  await tester.tap(find.text('__open__'));
  await tester.pumpAndSettle();
}

Future<void> mountApp(WidgetTester tester, {FakeApi? api}) async {
  tester.view.physicalSize = const Size(390, 844);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);
  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        apiProvider.overrideWithValue(api ?? seedApi()),
        localeProvider.overrideWith((ref) => 'en'),
      ],
      child: const NexusApp(),
    ),
  );
  await settle(tester);
}

void loadFonts() {
  setUpAll(() async {
    final loader = FontLoader('NotoSansMyanmar')
      ..addFont(rootBundle.load('assets/fonts/NotoSansMyanmar.ttf'));
    await loader.load();
    final icons = FontLoader('MaterialIcons')
      ..addFont(rootBundle.load('fonts/MaterialIcons-Regular.otf'));
    await icons.load();
    for (final code in ['en', 'my', 'zh']) {
      await rootBundle.loadString('assets/i18n/$code.json');
    }
  });
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  loadFonts();
  uiTests();
  mainTests();
  discoverTests();
  communityTests();
  chatTests();
  profileTests();
}

void uiTests() {
  group('ui', () {
    testWidgets('remote view shows error and retries', (tester) async {
      final api = seedApi()..failures.add('GET /v1/feed');
      await mount(
        tester,
        RemoteView(
          path: '/v1/feed',
          builder: (c, d, refresh) => Text('loaded ${items(d).length}'),
        ),
        api: api,
      );
      expect(find.byType(TextButton), findsOneWidget);
      expect(
        find.text(
          'Action failed. Check your connection and account permissions.',
        ),
        findsOneWidget,
      );
      api.failures.clear();
      await tester.tap(find.text('Try again'));
      await tester.pumpAndSettle();
      expect(find.text('loaded 0'), findsOneWidget);
    });

    testWidgets('remote view reloads when path changes', (tester) async {
      final api = seedApi()
        ..responses['GET /v1/a'] = {
          'items': [
            {'v': 'a'},
          ],
        };
      await mount(
        tester,
        RemoteView(
          path: '/v1/a',
          builder: (c, d, refresh) => Text('${d['items']}'),
        ),
        api: api,
      );
      expect(find.textContaining('a'), findsOneWidget);
      api.responses['GET /v1/b'] = {
        'items': [
          {'v': 'b'},
        ],
      };
      await tester.pumpWidget(
        app(
          api: api,
          home: RemoteView(
            path: '/v1/b',
            builder: (c, d, refresh) => Text('${d['items']}'),
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.textContaining('b'), findsOneWidget);
    });

    testWidgets('field toggles and chips render', (tester) async {
      final value = TextEditingController(text: 'x');
      addTearDown(value.dispose);
      var toggled = false;
      var range = 5;
      await mount(
        tester,
        PageFrame(
          title: 'Frame',
          child: ListView(
            children: [
              Field(controller: value, label: 'Name'),
              ToggleRow(
                label: 'Wheel',
                value: false,
                onChanged: (v) => toggled = v,
              ),
              BucketRange(value: range, onChanged: (v) => range = v),
              const Avatar(label: 'Nexus', tier: 2),
              const TownshipChip('kamayut'),
              const DistanceBucketLabel('1 km'),
              const EmptyState('empty'),
            ],
          ),
        ),
      );
      expect(find.text('Frame'), findsOneWidget);
      await tester.tap(find.byType(Switch));
      await tester.pumpAndSettle();
      expect(toggled, true);
      await tester.tap(find.text('5 km'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('3 km').last);
      await tester.pumpAndSettle();
      expect(range, 3);
      expect(find.text('N'), findsOneWidget);
      expect(find.text('kamayut'), findsOneWidget);
      expect(find.text('≈ 1 km'), findsOneWidget);
      expect(find.text('empty'), findsOneWidget);
    });

    testWidgets('info dialog and failed action notify', (tester) async {
      await mount(
        tester,
        Builder(
          builder: (c) => Column(
            children: [
              TextButton(
                onPressed: () => info(c, 'privacy'),
                child: const Text('info'),
              ),
              TextButton(
                onPressed: () => action(c, () async {
                  throw const ApiException(500, 'boom');
                }),
                child: const Text('fail'),
              ),
              TextButton(
                onPressed: () => notice(c, c.tr('done')),
                child: const Text('notice'),
              ),
            ],
          ),
        ),
      );
      await tester.tap(find.text('info'));
      await tester.pumpAndSettle();
      expect(find.textContaining('Approximate distance only'), findsOneWidget);
      await tester.tap(find.text('Close'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('fail'));
      await tester.pumpAndSettle();
      expect(
        find.text(
          'Action failed. Check your connection and account permissions.',
        ),
        findsOneWidget,
      );
      await tester.pump(const Duration(seconds: 5));
      await tester.tap(find.text('notice'));
      await tester.pumpAndSettle();
      expect(find.text('Done'), findsOneWidget);
    });

    testWidgets('safety sheet blocks a member', (tester) async {
      final api = seedApi();
      await mount(
        tester,
        Consumer(
          builder: (c, ref, _) => TextButton(
            onPressed: () => SafetySheet.show(c, ref, user: 'u1'),
            child: const Text('sheet'),
          ),
        ),
        api: api,
      );
      await tester.tap(find.text('sheet'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Block'));
      await tester.pumpAndSettle();
      expect(api.calls, contains('POST /v1/safety/block'));
      expect(api.lastBody?['target_id'], 'u1');
      expect(find.text('Done'), findsOneWidget);
    });

    testWidgets('safety sheet opens report page', (tester) async {
      final api = seedApi();
      await mount(
        tester,
        Consumer(
          builder: (c, ref, _) => TextButton(
            onPressed: () => SafetySheet.show(c, ref, profile: 'p1'),
            child: const Text('sheet'),
          ),
        ),
        api: api,
      );
      await tester.tap(find.text('sheet'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Report'));
      await tester.pumpAndSettle();
      expect(find.text('HARASSMENT'), findsOneWidget);
      await tester.tap(find.text('HARASSMENT'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('SCAM').last);
      await tester.pumpAndSettle();
      await tester.enterText(find.byType(TextField), 'spam');
      await tester.tap(find.widgetWithText(FilledButton, 'Report'));
      await tester.pumpAndSettle();
      expect(api.calls, contains('POST /v1/safety/report'));
      expect(api.lastBody?['profile_id'], 'p1');
      expect(api.lastBody?['category'], 'SCAM');
      expect(api.lastBody?['details'], 'spam');
    });

    testWidgets('report page failure keeps form', (tester) async {
      final api = seedApi()..failures.add('POST /v1/safety/report');
      await mountPushed(
        tester,
        const ReportPage(target: {'user': 'u'}),
        api: api,
      );
      await tester.tap(find.widgetWithText(FilledButton, 'Report'));
      await tester.pumpAndSettle();
      expect(api.calls, contains('POST /v1/safety/report'));
      expect(find.text('HARASSMENT'), findsOneWidget);
      expect(api.signedOut, false);
    });
  });
}

void mainTests() {
  group('main', () {
    testWidgets('welcome slides lead to sign in and verify', (tester) async {
      final api = seedApi()
        ..responses['POST /v1/auth/otp/request'] = {
          'challenge_id': 'ch1',
          'development_code': '123456',
        }
        ..responses['POST /v1/auth/otp/verify'] = {
          'access_token': 'a',
          'refresh_token': 'r',
          'user_id': 'u',
        };
      await mountApp(tester, api: api);
      expect(find.text('Meet people, not map pins'), findsOneWidget);
      await tester.tap(find.text('Continue'));
      await tester.pumpAndSettle();
      expect(find.text('Your city. Your community.'), findsOneWidget);
      await tester.tap(find.text('Continue'));
      await tester.pumpAndSettle();
      expect(find.text('Make plans. Meet safe.'), findsOneWidget);
      await tester.tap(find.text('Get started'));
      await tester.pumpAndSettle();
      expect(find.text('Phone sign in'), findsOneWidget);

      await tester.enterText(find.byType(TextField).first, '+959111111111');
      await tester.tap(find.text('Send code'));
      await tester.pumpAndSettle();
      expect(find.textContaining('DEVELOPMENT ONLY: 123456'), findsOneWidget);
      await tester.enterText(find.byType(TextField).at(1), '1990-01-01');
      await tester.enterText(find.byType(TextField).at(2), '123456');
      await tester.tap(find.text('Verify & sign in'));
      await tester.pumpAndSettle();
      expect(find.byType(NavigationBar), findsOneWidget);
      expect(api.access, 'a');
    });

    testWidgets('welcome skip opens sign in', (tester) async {
      await mountApp(tester, api: seedApi());
      await tester.tap(find.text('Skip'));
      await tester.pumpAndSettle();
      expect(find.text('Phone sign in'), findsOneWidget);
    });

    testWidgets('sign in retry clears challenge and errors notify', (
      tester,
    ) async {
      final api = seedApi()
        ..responses['POST /v1/auth/otp/request'] = {'challenge_id': 'ch1'};
      await mount(tester, const SignIn(), api: api);
      await tester.tap(find.text('Send code'));
      await tester.pumpAndSettle();
      expect(find.text('Six-digit verification code'), findsOneWidget);
      await tester.tap(find.text('Try again'));
      await tester.pumpAndSettle();
      expect(find.text('Six-digit verification code'), findsNothing);
      api.failures.add('POST /v1/auth/otp/request');
      await tester.tap(find.text('Send code'));
      await tester.pumpAndSettle();
      expect(
        find.text(
          'Action failed. Check your connection and account permissions.',
        ),
        findsOneWidget,
      );
    });

    testWidgets('profile gate routes new users to setup', (tester) async {
      final api = seedApi()
        ..access = 'a'
        ..responses['GET /v1/profile'] = const ApiException(404, 'no_profile');
      await mountApp(tester, api: api);
      expect(find.text('Complete your social profile'), findsOneWidget);
    });

    testWidgets('profile gate failure offers sign out', (tester) async {
      final api = seedApi()
        ..access = 'a'
        ..failures.add('GET /v1/profile');
      await mountApp(tester, api: api);
      await tester.tap(find.text('Sign out'));
      await tester.pumpAndSettle();
      expect(api.signedOut, true);
      expect(find.text('Skip'), findsOneWidget);
    });

    testWidgets('home switches tabs and opens global actions', (tester) async {
      final api = seedApi();
      await mount(tester, const Home(), api: api);
      expect(find.byType(NavigationBar), findsOneWidget);
      for (final label in ['Community', 'Chats', 'Me']) {
        await tester.tap(
          find.descendant(
            of: find.byType(NavigationBar),
            matching: find.text(label),
          ),
        );
        await tester.pumpAndSettle();
      }
      expect(find.text('Sign out'), findsOneWidget);
      await tester.tap(find.byTooltip('Notifications'));
      await tester.pumpAndSettle();
      expect(find.text('Notifications'), findsOneWidget);
      await tester.pageBack();
      await tester.pumpAndSettle();
      await tester.tap(find.byTooltip('Safety Center'));
      await tester.pumpAndSettle();
      expect(find.text('Ghost mode — hide & lock chats'), findsOneWidget);
      await tester.pageBack();
      await tester.pumpAndSettle();
      await tester.tap(find.byTooltip('Create'));
      await tester.pumpAndSettle();
      expect(find.text('Audience'), findsOneWidget);
    });
  });
}

Map<String, dynamic> cardData({String id = 'p1'}) => {
  'profile_id': id,
  'nickname': 'Mya',
  'age': 27,
  'distance_bucket': '1 km',
  'township': 'kamayut',
  'bio': 'Hello',
  'interests': ['music'],
  'prompts': ['Coffee'],
};

void discoverTests() {
  group('discover', () {
    testWidgets('cards swipe like and pass', (tester) async {
      final api = seedApi()
        ..responses['GET /v1/discover'] = {
          'items': [cardData()],
        }
        ..responses['POST /v1/actions/swipe'] = {'matched': true};
      await mount(tester, const Discover(), api: api);
      expect(find.byType(ProfileCard), findsOneWidget);
      await tester.ensureVisible(find.text('Like'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Like'));
      await tester.pumpAndSettle();
      expect(
        api.calls.where((c) => c == 'POST /v1/actions/swipe'),
        hasLength(1),
      );
      expect(api.lastBody?['intent'], 'LIKE');
      expect(find.text('It is a match! Open Chats.'), findsOneWidget);
      await tester.ensureVisible(find.text('Pass'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Pass'));
      await tester.pumpAndSettle();
      expect(api.lastBody?['intent'], 'PASS');
    });

    testWidgets('nearby mode lists profiles', (tester) async {
      final api = seedApi()
        ..responses['GET /v1/lookaround?band=500m'] = {
          'items': [cardData(id: 'p2')],
        };
      await mount(tester, const Discover(), api: api);
      await tester.tap(find.text('Look Around'));
      await tester.pumpAndSettle();
      expect(find.byType(ProfileCard), findsOneWidget);
      expect(find.textContaining('Mya'), findsOneWidget);
    });

    testWidgets('radar blip opens profile sheet', (tester) async {
      final api = seedApi()
        ..responses['GET /v1/radar'] = {
          'items': [
            {
              'blip_id': 'b1',
              'display_angle': 90,
              'display_ring': 1,
              'distance_bucket': '1 km',
            },
          ],
        }
        ..responses['GET /v1/radar/blips/b1'] = cardData(id: 'p3');
      await mount(tester, const Discover(), api: api);
      await tester.tap(find.text('Radar'));
      await tester.pumpAndSettle();
      expect(find.byType(RadarView), findsOneWidget);
      await tester.tap(find.byType(Blip));
      await tester.pumpAndSettle();
      expect(find.text('Mya, 27'), findsOneWidget);
    });

    testWidgets('location button updates the fix', (tester) async {
      final api = seedApi();
      await mount(tester, const Discover(), api: api);
      await tester.tap(find.text('Update location'));
      await tester.pumpAndSettle();
      expect(
        find.textContaining('Location is used only with permission'),
        findsOneWidget,
      );
      await tester.tap(find.text('Close'));
      await tester.pumpAndSettle();
      expect(api.locationUpdates, 1);
    });

    testWidgets('long press opens safety sheet and blocks', (tester) async {
      final api = seedApi()
        ..responses['GET /v1/discover'] = {
          'items': [cardData()],
        };
      await mount(tester, const Discover(), api: api);
      await tester.longPress(find.text('Mya, 27'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Block'));
      await tester.pumpAndSettle();
      expect(api.calls, contains('POST /v1/safety/block'));
      expect(api.lastBody?['profile_id'], 'p1');
    });
  });
}

Future<void> scrollTo(WidgetTester tester, Finder finder) async {
  await tester.scrollUntilVisible(
    finder,
    200,
    scrollable: find.byType(Scrollable).first,
  );
  await tester.pumpAndSettle();
}

void pressIcon(WidgetTester tester, IconData icon) {
  final button = tester.widget<IconButton>(
    find.ancestor(of: find.byIcon(icon), matching: find.byType(IconButton)),
  );
  button.onPressed!();
}

Map<String, dynamic> postData() => {
  'id': 'post1',
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
};

void communityTests() {
  group('community', () {
    testWidgets('post reactions, share, bookmark and comments', (tester) async {
      final api = seedApi()
        ..responses['GET /v1/feed'] = {
          'items': [postData()],
        }
        ..responses['GET /v1/posts/post1/comments'] = {
          'items': [
            {
              'id': 'c1',
              'author': 'Root Author',
              'body': 'Root',
              'parent_comment_id': null,
              'status': 'LIVE',
            },
            {
              'id': 'c2',
              'author': 'Reply Author',
              'body': 'Nested',
              'parent_comment_id': 'c1',
              'status': 'UNDER_REVIEW',
            },
          ],
        };
      await mount(tester, const Community(), api: api);
      expect(find.byType(PostCard), findsOneWidget);
      await tester.tap(find.text('Like'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('LOVE').last);
      await tester.pumpAndSettle();
      expect(api.calls, contains('POST /v1/posts/post1/reactions'));
      expect(api.lastBody?['type'], 'LOVE');
      await tester.tap(find.text('Hello'));
      await tester.tap(find.text('Hello'));
      await tester.pumpAndSettle();
      await scrollTo(tester, find.text('Share'));
      await tester.tap(find.text('Share'));
      await tester.pumpAndSettle();
      expect(api.calls, contains('POST /v1/posts/post1/shares'));
      await tester.tap(find.byTooltip('Bookmark'));
      await tester.pumpAndSettle();
      expect(api.calls, contains('POST /v1/posts/post1/bookmark'));
      await scrollTo(tester, find.text('Comment'));
      await tester.tap(find.text('Comment'));
      await tester.pumpAndSettle();
      await tester.pump(const Duration(seconds: 5));
      expect(find.text('Root'), findsOneWidget);
      expect(find.text('Nested'), findsOneWidget);
      expect(find.text('Under review — only you can see this'), findsOneWidget);
      await tester.tap(find.byIcon(Icons.reply));
      await tester.pumpAndSettle();
      expect(find.text('Cancel'), findsOneWidget);
      await tester.tap(find.text('Cancel'));
      await tester.pumpAndSettle();
      await tester.tap(find.byIcon(Icons.reply));
      await tester.pumpAndSettle();
      await tester.enterText(find.byType(TextField), 'Reply text');
      await tester.pump();
      pressIcon(tester, Icons.send_outlined);
      await tester.pumpAndSettle();
      expect(api.calls, contains('POST /v1/posts/post1/comments'));
      expect(api.lastBody?['parent_comment_id'], 'c1');
      expect(api.lastBody?['body'], 'Reply text');
    });

    testWidgets('events and groups segment actions', (tester) async {
      final api = seedApi()
        ..responses['GET /v1/events'] = {
          'items': [
            {
              'id': 'e1',
              'title': 'Meetup',
              'township': 'kamayut',
              'starts_at': '2026-12-01T10:00:00Z',
              'description': 'Coffee',
              'venue_area': 'Downtown',
            },
          ],
        }
        ..responses['GET /v1/groups'] = {
          'items': [
            {
              'id': 'g1',
              'name': 'Readers',
              'description': 'Books',
              'township': 'bahan',
              'joined': false,
            },
          ],
        };
      await mount(tester, const Community(), api: api);
      await tester.tap(find.text('Events'));
      await tester.pumpAndSettle();
      expect(find.text('Meetup'), findsOneWidget);
      await tester.tap(find.text('Attend'));
      await tester.pumpAndSettle();
      expect(api.lastBody?['status'], 'ATTEND');
      await tester.tap(find.text('Not going'));
      await tester.pumpAndSettle();
      expect(api.lastBody?['status'], 'NOT_GOING');
      await tester.tap(find.text('Groups'));
      await tester.pumpAndSettle();
      expect(find.text('Readers'), findsOneWidget);
      await tester.tap(find.text('Join'));
      await tester.pumpAndSettle();
      expect(api.calls, contains('POST /v1/groups/g1/join'));
    });

    testWidgets('compose publishes and pops', (tester) async {
      final api = seedApi();
      await mountPushed(tester, const ComposePost(), api: api);
      await tester.tap(find.text('Public'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Community').last);
      await tester.pumpAndSettle();
      await tester.tap(find.text('kamayut'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('bahan').last);
      await tester.pumpAndSettle();
      await tester.enterText(find.byType(TextField), 'Post body');
      await tester.tap(find.text('Submit for review'));
      await tester.pumpAndSettle();
      expect(api.calls, contains('POST /v1/posts'));
      expect(api.lastBody?['body'], 'Post body');
      expect(api.lastBody?['audience'], 'COMMUNITY');
      expect(api.lastBody?['township'], 'bahan');
      expect(find.text('__open__'), findsOneWidget);
    });

    testWidgets('search filters by segment', (tester) async {
      final api = seedApi()
        ..responses['GET /v1/search?type=people&q=ab'] = {
          'items': [
            {'real_name': 'Abby'},
          ],
        }
        ..responses['GET /v1/search?type=posts&q=ab'] = {
          'items': [
            {'body': 'Ab post'},
          ],
        };
      await mount(tester, const SearchPage(), api: api);
      expect(find.text('Search'), findsWidgets);
      await tester.enterText(find.byType(TextField), 'ab');
      await tester.tap(find.byIcon(Icons.search));
      await tester.pumpAndSettle();
      expect(find.text('Abby'), findsOneWidget);
      await tester.tap(find.text('Posts'));
      await tester.pumpAndSettle();
      expect(find.text('Ab post'), findsOneWidget);
      await tester.tap(find.text('Events'));
      await tester.pumpAndSettle();
      expect(find.text('Nothing here yet'), findsOneWidget);
    });
  });
}

Map<String, dynamic> chatMessages() => {
  'items': [
    {'id': 'm1', 'seq': 1, 'sender': 'me', 'mode': 'NORMAL', 'text': 'Hi'},
    {'id': 'm2', 'seq': 2, 'sender': 'other', 'mode': 'WHISPER', 'text': ''},
  ],
};

void chatTests() {
  group('chat', () {
    testWidgets('chat list opens a room and sends a whisper', (tester) async {
      final api = seedApi()
        ..userId = 'me'
        ..failures.add('POST /v1/realtime/ticket')
        ..responses['GET /v1/chats'] = {
          'items': [
            {'id': 'room-12345678', 'kind': 'SOCIAL'},
          ],
        }
        ..responses['GET /v1/chats/room-12345678/messages'] = chatMessages();
      await mount(tester, const ChatList(), api: api);
      expect(find.text('SOCIAL'), findsOneWidget);
      await tester.tap(find.text('SOCIAL'));
      await tester.pumpAndSettle();
      expect(find.text('Hi'), findsOneWidget);
      await tester.tap(find.text('Read & expire'));
      await tester.pump();
      await tester.pump(const Duration(seconds: 1));
      expect(api.calls, contains('POST /v1/chats/room-12345678/read'));
      expect(find.text('Read & expire'), findsNothing);
      await tester.tap(find.byType(Switch));
      await tester.pumpAndSettle();
      expect(
        find.textContaining('Whisper text is deleted from the live server'),
        findsWidgets,
      );
      await tester.tap(find.text('Close'));
      await tester.pumpAndSettle();
      await tester.enterText(find.byType(TextField), 'Secret');
      await tester.tap(find.byTooltip('Send'));
      await tester.pumpAndSettle();
      expect(api.calls, contains('POST /v1/chats/room-12345678/messages'));
      final sent = api.bodies['POST /v1/chats/room-12345678/messages'];
      expect(sent?['mode'], 'WHISPER');
      expect(sent?['text'], 'Secret');
      await tester.pumpWidget(const SizedBox());
      await tester.pump(const Duration(seconds: 2));
    });

    testWidgets('chat page errors and lifecycle release the socket', (
      tester,
    ) async {
      final api = seedApi()
        ..userId = 'me'
        ..failures.addAll([
          'POST /v1/realtime/ticket',
          'GET /v1/chats/room1/messages',
        ]);
      await mount(tester, const ChatPage(room: 'room1'), api: api);
      expect(
        find.text(
          'Action failed. Check your connection and account permissions.',
        ),
        findsOneWidget,
      );
      await tester.tap(find.byTooltip('Refresh'));
      await tester.pumpAndSettle();
      tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.inactive);
      tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.hidden);
      tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.paused);
      await tester.pumpAndSettle();
      tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.hidden);
      tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.inactive);
      tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
      await tester.pumpAndSettle();
      await tester.tap(find.byTooltip('Calls'));
      await tester.pumpAndSettle();
      expect(find.textContaining('Not enabled'), findsOneWidget);
      await tester.tap(find.text('Close'));
      await tester.pumpAndSettle();
      await tester.pumpWidget(const SizedBox());
      await tester.pump(const Duration(seconds: 2));
    });

    testWidgets('capture alert posts metadata', (tester) async {
      final api = seedApi()
        ..userId = 'me'
        ..failures.add('POST /v1/realtime/ticket');
      await mount(tester, const ChatPage(room: 'room1'), api: api);
      await tester.binding.defaultBinaryMessenger.handlePlatformMessage(
        'app.nexus/safety',
        const StandardMethodCodec().encodeMethodCall(
          const MethodCall('capture'),
        ),
        (_) {},
      );
      await tester.pumpAndSettle();
      expect(api.calls, contains('POST /v1/safety/capture-alert'));
      expect(api.lastBody?['kind'], 'SCREENSHOT');
      await tester.pumpWidget(const SizedBox());
      await tester.pump(const Duration(seconds: 2));
    });
  });
}

void profileTests() {
  group('profile', () {
    testWidgets('me page navigates editors, exports and signs out', (
      tester,
    ) async {
      final api = seedApi()
        ..responses['GET /v1/dating-profile/preview'] = {
          'nickname': 'Mya',
          'bio': 'Hi',
          'prompts': ['Coffee'],
          'interests': ['music'],
          'gender': 'woman',
          'preferences': [],
          'radar_opt_in': false,
          'hide_from_social_friends': true,
          'radius_km': 5,
        }
        ..responses['GET /v1/account/export'] = {'profile': 'data'}
        ..responses['GET /v1/auth/sessions'] = {
          'items': [
            {
              'id': 's1',
              'current': true,
              'device': 'Pixel',
              'expires_at': '2026-12-01',
            },
          ],
        }
        ..responses['GET /v1/safety/reports'] = {
          'items': [
            {'category': 'SCAM', 'status': 'OPEN', 'severity': 'LOW'},
          ],
        }
        ..responses['GET /v1/safety/blocks'] = {
          'items': [
            {'target_id': 'u9'},
          ],
        };
      await mount(tester, const MePage(), api: api);
      expect(find.text('Social'), findsOneWidget);
      await tester.tap(find.text('Social'));
      await tester.pumpAndSettle();
      expect(find.text('Social real name'), findsOneWidget);
      await tester.pageBack();
      await tester.pumpAndSettle();
      await tester.tap(find.text('Dating'));
      await tester.pumpAndSettle();
      expect(find.text('Dating nickname (not real name)'), findsOneWidget);
      await tester.pageBack();
      await tester.pumpAndSettle();
      await tester.tap(find.text('Verification'));
      await tester.pumpAndSettle();
      expect(find.textContaining('Not enabled'), findsOneWidget);
      await tester.tap(find.text('Close'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Nexus+'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Close'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Safety Center'));
      await tester.pumpAndSettle();
      expect(find.text('My reports'), findsOneWidget);
      expect(find.text('SCAM'), findsOneWidget);
      expect(find.text('u9'), findsOneWidget);
      await tester.pageBack();
      await tester.pumpAndSettle();
      await tester.tap(find.text('Sessions'));
      await tester.pumpAndSettle();
      expect(find.text('Pixel'), findsOneWidget);
      await tester.pageBack();
      await tester.pumpAndSettle();
      await scrollTo(tester, find.text('Export my data'));
      await tester.tap(find.text('Export my data'));
      await tester.pumpAndSettle();
      expect(find.textContaining('"profile": "data"'), findsOneWidget);
      await tester.tap(find.text('Close'));
      await tester.pumpAndSettle();
      await scrollTo(tester, find.text('Sign out'));
      await tester.tap(find.text('Sign out'));
      await tester.pumpAndSettle();
      expect(api.calls, contains('DELETE /v1/auth/sessions/s1'));
      expect(api.signedOut, true);
    });

    testWidgets('social editor saves and completes setup', (tester) async {
      final api = seedApi();
      await mountPushed(tester, const SocialEditor(), api: api);
      expect(find.text('Social Person'), findsOneWidget);
      await tester.enterText(find.byType(TextField).first, 'New Name');
      await tester.tap(find.text('Save'));
      await tester.pumpAndSettle();
      expect(api.calls, contains('PUT /v1/profile'));
      expect(api.lastBody?['real_name'], 'New Name');
      expect(find.text('__open__'), findsOneWidget);

      await mount(tester, const SocialEditor(initial: true), api: api);
      await tester.tap(find.text('Save'));
      await tester.pumpAndSettle();
      expect(api.updatedProfile, true);
    });

    testWidgets('dating editor saves preferences', (tester) async {
      final api = seedApi()
        ..responses['GET /v1/dating-profile/preview'] = {
          'nickname': 'Mya',
          'bio': 'Hi',
          'prompts': ['Coffee'],
          'interests': ['music'],
          'gender': 'woman',
          'preferences': [],
          'radar_opt_in': false,
          'hide_from_social_friends': true,
          'radius_km': 5,
        };
      await mountPushed(tester, const DatingEditor(), api: api);
      expect(find.text('Mya'), findsOneWidget);
      await tester.tap(find.byType(Switch).first);
      await tester.pumpAndSettle();
      await tester.tap(find.byType(Switch).last);
      await tester.pumpAndSettle();
      await tester.tap(find.text('5 km'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('3 km').last);
      await tester.pumpAndSettle();
      await scrollTo(tester, find.text('Save'));
      await tester.tap(find.text('Save'));
      await tester.pumpAndSettle();
      expect(api.calls, contains('PUT /v1/dating-profile'));
      expect(api.lastBody?['radar_opt_in'], true);
      expect(api.lastBody?['radius_km'], 3);
      expect(find.text('__open__'), findsOneWidget);
    });

    testWidgets('safety center toggles ghost and unblocks', (tester) async {
      final api = seedApi()
        ..responses['GET /v1/safety/reports'] = {
          'items': [
            {'category': 'HARASSMENT', 'status': 'OPEN', 'severity': 'HIGH'},
          ],
        }
        ..responses['GET /v1/safety/blocks'] = {
          'items': [
            {'target_id': 'u9'},
          ],
        };
      await mount(tester, const SafetyCenter(), api: api);
      await tester.tap(find.byType(Switch));
      await tester.pumpAndSettle();
      expect(api.calls, contains('POST /v1/safety/ghost'));
      expect(api.lastBody?['enabled'], true);
      expect(find.text('HARASSMENT'), findsOneWidget);
      await tester.tap(find.text('Unblock'));
      await tester.pumpAndSettle();
      expect(api.calls, contains('DELETE /v1/safety/blocks/u9'));
    });

    testWidgets('inbox lists notifications', (tester) async {
      final api = seedApi()
        ..responses['GET /v1/notifications'] = {
          'items': [
            {'kind': 'REPORT_UPDATE', 'category': 'SAFETY'},
            {'kind': 'FOLLOW', 'category': 'SOCIAL'},
          ],
        };
      await mount(tester, const Inbox(), api: api);
      expect(find.text('REPORT_UPDATE'), findsOneWidget);
      expect(find.text('FOLLOW'), findsOneWidget);
    });

    testWidgets('sessions page revokes others and current', (tester) async {
      final api = seedApi()
        ..responses['GET /v1/auth/sessions'] = {
          'items': [
            {
              'id': 's1',
              'current': true,
              'device': 'Pixel',
              'expires_at': '2026-12-01',
            },
            {
              'id': 's2',
              'current': false,
              'device': 'Tablet',
              'expires_at': '2026-12-01',
            },
          ],
        };
      await mountPushed(tester, const SessionsPage(), api: api);
      await tester.tap(find.byIcon(Icons.close).at(1));
      await tester.pumpAndSettle();
      expect(api.calls, contains('DELETE /v1/auth/sessions/s2'));
      await tester.tap(find.byIcon(Icons.close).first);
      await tester.pumpAndSettle();
      expect(api.calls, contains('DELETE /v1/auth/sessions/s1'));
      expect(api.signedOut, true);
      expect(find.text('__open__'), findsOneWidget);
    });

    testWidgets('delete page requires the confirmation phrase', (tester) async {
      final api = seedApi();
      await mount(tester, const DeletePage(), api: api);
      expect(
        tester.widget<FilledButton>(find.byType(FilledButton)).onPressed,
        isNull,
      );
      await tester.enterText(find.byType(TextField), 'DELETE MY ACCOUNT');
      await tester.pump();
      await tester.tap(find.byType(FilledButton));
      await tester.pumpAndSettle();
      expect(api.calls, contains('DELETE /v1/account'));
      expect(api.lastBody?['confirm'], 'DELETE MY ACCOUNT');
      expect(api.signedOut, true);
    });
  });
}
