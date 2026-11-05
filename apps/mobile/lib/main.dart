import 'package:flutter/material.dart';
import 'package:flutter_localizations/flutter_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'api.dart';
import 'l10n.dart';
import 'ui.dart';
import 'discover.dart';
import 'community.dart';
import 'profile.dart';
import 'chat.dart';

void main() {
  WidgetsFlutterBinding.ensureInitialized();
  runApp(const ProviderScope(child: NexusApp()));
}

class NexusApp extends ConsumerWidget {
  const NexusApp({super.key});
  @override
  Widget build(BuildContext c, WidgetRef ref) => MaterialApp(
    title: 'Nexus',
    debugShowCheckedModeBanner: false,
    locale: Locale(ref.watch(localeProvider)),
    supportedLocales: const [Locale('en'), Locale('my'), Locale('zh')],
    localizationsDelegates: const [
      Strings.delegate,
      GlobalMaterialLocalizations.delegate,
      GlobalWidgetsLocalizations.delegate,
      GlobalCupertinoLocalizations.delegate,
    ],
    theme: ThemeData(
      useMaterial3: true,
      colorScheme: ColorScheme.fromSeed(
        seedColor: primary,
        primary: primary,
        surface: Colors.white,
      ),
      scaffoldBackgroundColor: const Color(0xFFF5F7FB),
      fontFamily: 'NotoSansMyanmar',
      textTheme: const TextTheme(
        bodyMedium: TextStyle(fontSize: 15, height: 1.6, color: ink),
        bodyLarge: TextStyle(fontSize: 16, height: 1.6, color: ink),
      ),
      appBarTheme: const AppBarTheme(
        backgroundColor: Colors.white,
        foregroundColor: ink,
      ),
      cardTheme: CardThemeData(
        color: Colors.white,
        elevation: 0,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(16)),
      ),
      inputDecorationTheme: InputDecorationTheme(
        filled: true,
        fillColor: Colors.white,
        border: OutlineInputBorder(
          borderRadius: BorderRadius.circular(12),
          borderSide: const BorderSide(color: Color(0xFFE1E6F0)),
        ),
      ),
      filledButtonTheme: FilledButtonThemeData(
        style: FilledButton.styleFrom(
          minimumSize: const Size(48, 52),
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(12),
          ),
        ),
      ),
    ),
    home: const SessionGate(),
  );
}

class SessionGate extends ConsumerStatefulWidget {
  const SessionGate({super.key});
  @override
  ConsumerState<SessionGate> createState() => _SessionGateState();
}

class _SessionGateState extends ConsumerState<SessionGate> {
  late Future<void> ready;
  @override
  void initState() {
    super.initState();
    ready = ref.read(apiProvider).restore();
  }

  @override
  Widget build(BuildContext c) => FutureBuilder<void>(
    future: ready,
    builder: (c, s) {
      if (s.connectionState != ConnectionState.done) {
        return const Scaffold(body: Center(child: CircularProgressIndicator()));
      }
      return ListenableBuilder(
        listenable: ref.watch(apiProvider),
        builder: (c, _) => ref.read(apiProvider).signedIn
            ? const ProfileGate()
            : const Welcome(),
      );
    },
  );
}

class ProfileGate extends ConsumerWidget {
  const ProfileGate({super.key});
  @override
  Widget build(BuildContext c, WidgetRef ref) => FutureBuilder<Data>(
    future: ref.read(apiProvider).request('GET', '/v1/profile'),
    builder: (c, s) {
      if (s.connectionState != ConnectionState.done) {
        return const Scaffold(body: Center(child: CircularProgressIndicator()));
      }
      if (s.hasError) {
        if (s.error is ApiException &&
            (s.error as ApiException).status == 404) {
          return const SocialEditor(initial: true);
        }
        return PageFrame(
          title: 'Nexus',
          child: Column(
            mainAxisAlignment: MainAxisAlignment.center,
            children: [
              Text(c.tr('error')),
              TextButton(
                onPressed: () => ref.read(apiProvider).logout(),
                child: Text(c.tr('logout')),
              ),
            ],
          ),
        );
      }
      return const Home();
    },
  );
}

class Welcome extends ConsumerStatefulWidget {
  const Welcome({super.key});
  @override
  ConsumerState<Welcome> createState() => _WelcomeState();
}

class _WelcomeState extends ConsumerState<Welcome> {
  int index = 0;
  @override
  Widget build(BuildContext c) => PageFrame(
    title: 'Nexus',
    actions: [
      DropdownButton<String>(
        value: ref.watch(localeProvider),
        items: [
          'my',
          'en',
          'zh',
        ].map((v) => DropdownMenuItem(value: v, child: Text(v))).toList(),
        onChanged: (v) => ref.read(localeProvider.notifier).state = v!,
      ),
    ],
    child: Padding(
      padding: const EdgeInsets.all(32),
      child: Column(
        children: [
          Align(
            alignment: Alignment.centerRight,
            child: TextButton(
              onPressed: () => open(c, const SignIn()),
              child: Text(c.tr('skip')),
            ),
          ),
          const Spacer(),
          Container(
            width: 180,
            height: 180,
            decoration: BoxDecoration(
              color: primary.withValues(alpha: .08),
              shape: BoxShape.circle,
            ),
            child: Icon(
              [
                Icons.radar,
                Icons.groups_outlined,
                Icons.shield_outlined,
              ][index],
              size: 96,
              color: primary,
            ),
          ),
          const SizedBox(height: 40),
          Text(
            c.tr('slide${index + 1}'),
            textAlign: TextAlign.center,
            style: const TextStyle(
              fontSize: 28,
              fontWeight: FontWeight.bold,
              color: ink,
            ),
          ),
          const SizedBox(height: 20),
          Text(c.tr('tagline'), textAlign: TextAlign.center),
          const Spacer(),
          Row(
            mainAxisAlignment: MainAxisAlignment.center,
            children: List.generate(
              3,
              (i) => Container(
                margin: const EdgeInsets.all(4),
                width: i == index ? 24 : 8,
                height: 6,
                decoration: BoxDecoration(
                  color: i == index ? primary : primary.withValues(alpha: .15),
                  borderRadius: BorderRadius.circular(20),
                ),
              ),
            ),
          ),
          const SizedBox(height: 24),
          SizedBox(
            width: double.infinity,
            child: FilledButton(
              onPressed: () {
                if (index < 2) {
                  setState(() => index++);
                } else {
                  open(c, const SignIn());
                }
              },
              child: Text(c.tr(index == 2 ? 'start' : 'next')),
            ),
          ),
        ],
      ),
    ),
  );
}

class SignIn extends ConsumerStatefulWidget {
  const SignIn({super.key});
  @override
  ConsumerState<SignIn> createState() => _SignInState();
}

class _SignInState extends ConsumerState<SignIn> {
  final phone = TextEditingController(text: '+95');
  final code = TextEditingController();
  final dob = TextEditingController();
  String? challenge, devCode;
  bool busy = false;
  @override
  void dispose() {
    phone.dispose();
    code.dispose();
    dob.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext c) => PageFrame(
    title: c.tr('signIn'),
    child: ListView(
      padding: const EdgeInsets.all(24),
      children: [
        const SizedBox(height: 32),
        const Icon(Icons.verified_user_outlined, size: 64, color: primary),
        const SizedBox(height: 32),
        Text(c.tr('adult')),
        const SizedBox(height: 24),
        Field(
          controller: phone,
          label: c.tr('phone'),
          keyboard: TextInputType.phone,
        ),
        Field(
          controller: dob,
          label: c.tr('dob'),
          keyboard: TextInputType.datetime,
        ),
        if (challenge != null)
          Field(
            controller: code,
            label: c.tr('otp'),
            keyboard: TextInputType.number,
          ),
        if (devCode != null) SelectableText('DEVELOPMENT ONLY: $devCode'),
        const SizedBox(height: 16),
        FilledButton(
          onPressed: busy
              ? null
              : () async {
                  setState(() => busy = true);
                  await action(c, () async {
                    final api = ref.read(apiProvider);
                    if (challenge == null) {
                      final d = await api.request(
                        'POST',
                        '/v1/auth/otp/request',
                        body: {'phone': phone.text.trim()},
                      );
                      if (mounted) {
                        setState(() {
                          challenge = d['challenge_id'] as String;
                          devCode = d['development_code'] as String?;
                        });
                      }
                    } else {
                      final d = await api.request(
                        'POST',
                        '/v1/auth/otp/verify',
                        body: {
                          'challenge_id': challenge,
                          'code': code.text.trim(),
                          'dob': dob.text.trim(),
                          'device': 'Nexus mobile',
                        },
                      );
                      await api.accept(d);
                      if (c.mounted) Navigator.of(c).popUntil((r) => r.isFirst);
                    }
                  });
                  if (mounted) setState(() => busy = false);
                },
          child: Text(c.tr(challenge == null ? 'requestOtp' : 'verify')),
        ),
        if (challenge != null)
          TextButton(
            onPressed: busy
                ? null
                : () => setState(() {
                    challenge = null;
                    devCode = null;
                  }),
            child: Text(c.tr('retry')),
          ),
      ],
    ),
  );
}

class Home extends ConsumerStatefulWidget {
  const Home({super.key});
  @override
  ConsumerState<Home> createState() => _HomeState();
}

class _HomeState extends ConsumerState<Home> {
  int index = 0;
  @override
  Widget build(BuildContext c) => Scaffold(
    appBar: AppBar(
      title: Text(
        index == 0
            ? 'Nexus'
            : c.tr(['discover', 'community', 'chats', 'me'][index]),
      ),
      actions: [
        IconButton(
          tooltip: c.tr('notifications'),
          icon: const Icon(Icons.notifications_none),
          onPressed: () => open(c, const Inbox()),
        ),
        IconButton(
          tooltip: c.tr('safety'),
          icon: const Icon(Icons.shield_outlined),
          onPressed: () => open(c, const SafetyCenter()),
        ),
      ],
    ),
    body: SafeArea(
      child: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 640),
          child: [
            const Discover(),
            const Community(),
            const ChatList(),
            const MePage(),
          ][index],
        ),
      ),
    ),
    floatingActionButton: FloatingActionButton(
      tooltip: c.tr('create'),
      onPressed: () => open(c, const ComposePost()),
      child: const Icon(Icons.add),
    ),
    floatingActionButtonLocation: FloatingActionButtonLocation.centerDocked,
    bottomNavigationBar: NavigationBar(
      selectedIndex: index,
      onDestinationSelected: (v) => setState(() => index = v),
      destinations: [
        NavigationDestination(
          icon: const Icon(Icons.explore_outlined),
          label: c.tr('discover'),
        ),
        NavigationDestination(
          icon: const Icon(Icons.people_outline),
          label: c.tr('community'),
        ),
        NavigationDestination(
          icon: const Icon(Icons.chat_bubble_outline),
          label: c.tr('chats'),
        ),
        NavigationDestination(
          icon: const Icon(Icons.person_outline),
          label: c.tr('me'),
        ),
      ],
    ),
  );
}
