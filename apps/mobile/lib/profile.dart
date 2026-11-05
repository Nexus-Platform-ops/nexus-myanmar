import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'api.dart';
import 'l10n.dart';
import 'ui.dart';

class MePage extends ConsumerWidget {
  const MePage({super.key});
  @override
  Widget build(BuildContext c, WidgetRef ref) => ListView(
    padding: const EdgeInsets.all(24),
    children: [
      const Center(child: Avatar(label: 'Nexus')),
      const SizedBox(height: 24),
      Text(c.tr('preview'), textAlign: TextAlign.center),
      const SizedBox(height: 16),
      Panel(
        child: Column(
          children: [
            ListTile(
              leading: const Icon(Icons.person_outline),
              title: Text(c.tr('social')),
              trailing: const Icon(Icons.chevron_right),
              onTap: () => open(c, const SocialEditor()),
            ),
            ListTile(
              leading: const Icon(Icons.favorite_border),
              title: Text(c.tr('dating')),
              trailing: const Icon(Icons.chevron_right),
              onTap: () => open(c, const DatingEditor()),
            ),
            ListTile(
              leading: const Icon(Icons.verified_user_outlined),
              title: Text(c.tr('verification')),
              onTap: () => info(c, 'unavailable'),
            ),
            ListTile(
              leading: const Icon(Icons.star_outline),
              title: Text(c.tr('plus')),
              onTap: () => info(c, 'unavailable'),
            ),
          ],
        ),
      ),
      ListTile(
        leading: const Icon(Icons.shield_outlined),
        title: Text(c.tr('safety')),
        onTap: () => open(c, const SafetyCenter()),
      ),
      ListTile(
        title: Text(c.tr('sessions')),
        onTap: () => open(c, const SessionsPage()),
      ),
      DropdownButtonFormField<String>(
        initialValue: ref.watch(localeProvider),
        decoration: InputDecoration(labelText: c.tr('language')),
        items: [
          'my',
          'en',
          'zh',
        ].map((v) => DropdownMenuItem(value: v, child: Text(v))).toList(),
        onChanged: (v) => ref.read(localeProvider.notifier).state = v!,
      ),
      const SizedBox(height: 24),
      OutlinedButton(
        onPressed: () => action(c, () async {
          final data = await ref
              .read(apiProvider)
              .request('GET', '/v1/account/export');
          if (c.mounted) {
            await showDialog<void>(
              context: c,
              builder: (c) => AlertDialog(
                title: Text(c.tr('export')),
                content: SingleChildScrollView(
                  child: SelectableText(
                    const JsonEncoder.withIndent('  ').convert(data),
                  ),
                ),
                actions: [
                  TextButton(
                    onPressed: () => Navigator.pop(c),
                    child: Text(c.tr('close')),
                  ),
                ],
              ),
            );
          }
        }),
        child: Text(c.tr('export')),
      ),
      TextButton(
        onPressed: () => open(c, const DeletePage()),
        child: Text(c.tr('delete'), style: const TextStyle(color: Colors.red)),
      ),
      TextButton(
        onPressed: () => action(c, () async {
          final api = ref.read(apiProvider);
          final sessions = await api.request('GET', '/v1/auth/sessions');
          for (final v in items(sessions)) {
            if (v['current'] == true) {
              await api.request('DELETE', '/v1/auth/sessions/${v['id']}');
            }
          }
          await api.logout();
        }),
        child: Text(c.tr('logout')),
      ),
    ],
  );
}

class SocialEditor extends ConsumerStatefulWidget {
  final bool initial;
  const SocialEditor({super.key, this.initial = false});
  @override
  ConsumerState<SocialEditor> createState() => _SocialEditorState();
}

class _SocialEditorState extends ConsumerState<SocialEditor> {
  final name = TextEditingController(), bio = TextEditingController();
  String town = 'kamayut';
  bool busy = false, loading = true;
  @override
  void initState() {
    super.initState();
    load();
  }

  Future<void> load() async {
    try {
      final d = await ref.read(apiProvider).request('GET', '/v1/profile');
      name.text = d['real_name'] as String;
      bio.text = d['bio'] as String;
      town = d['township'] as String;
    } catch (_) {}
    if (mounted) setState(() => loading = false);
  }

  @override
  void dispose() {
    name.dispose();
    bio.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext c) => PageFrame(
    title: c.tr(widget.initial ? 'setup' : 'social'),
    child: loading
        ? const Center(child: CircularProgressIndicator())
        : ListView(
            padding: const EdgeInsets.all(24),
            children: [
              Field(controller: name, label: c.tr('name')),
              Field(controller: bio, label: c.tr('bio'), lines: 3),
              DropdownButtonFormField<String>(
                initialValue: town,
                decoration: InputDecoration(labelText: c.tr('township')),
                items: towns
                    .map((v) => DropdownMenuItem(value: v, child: Text(v)))
                    .toList(),
                onChanged: (v) => town = v!,
              ),
              const SizedBox(height: 24),
              FilledButton(
                onPressed: busy
                    ? null
                    : () async {
                        setState(() => busy = true);
                        await action(c, () async {
                          final api = ref.read(apiProvider);
                          await api.request(
                            'PUT',
                            '/v1/profile',
                            body: {
                              'real_name': name.text,
                              'bio': bio.text,
                              'township': town,
                            },
                          );
                          if (c.mounted) {
                            if (widget.initial) {
                              api.profileUpdated();
                            } else {
                              Navigator.pop(c);
                            }
                          }
                        });
                        if (mounted) setState(() => busy = false);
                      },
                child: Text(c.tr('save')),
              ),
            ],
          ),
  );
}

class DatingEditor extends ConsumerStatefulWidget {
  const DatingEditor({super.key});
  @override
  ConsumerState<DatingEditor> createState() => _DatingEditorState();
}

class _DatingEditorState extends ConsumerState<DatingEditor> {
  final nick = TextEditingController(),
      bio = TextEditingController(),
      prompt = TextEditingController(),
      interests = TextEditingController(),
      gender = TextEditingController(text: 'unspecified'),
      preference = TextEditingController();
  bool radar = false, hide = true, loading = true, busy = false;
  int radius = 5;
  @override
  void initState() {
    super.initState();
    load();
  }

  Future<void> load() async {
    try {
      final d = await ref
          .read(apiProvider)
          .request('GET', '/v1/dating-profile/preview');
      nick.text = d['nickname'] as String;
      bio.text = d['bio'] as String;
      prompt.text = (d['prompts'] as List).join('\n');
      interests.text = (d['interests'] as List).join(', ');
      gender.text = d['gender'] as String;
      preference.text = (d['preferences'] as List).join(', ');
      radar = d['radar_opt_in'] as bool;
      hide = d['hide_from_social_friends'] as bool;
      radius = (d['radius_km'] as num).toInt();
    } catch (_) {}
    if (mounted) setState(() => loading = false);
  }

  @override
  void dispose() {
    for (final v in [nick, bio, prompt, interests, gender, preference]) {
      v.dispose();
    }
    super.dispose();
  }

  List<String> split(String s) =>
      s.split(',').map((v) => v.trim()).where((v) => v.isNotEmpty).toList();
  @override
  Widget build(BuildContext c) => PageFrame(
    title: c.tr('dating'),
    child: loading
        ? const Center(child: CircularProgressIndicator())
        : ListView(
            padding: const EdgeInsets.all(24),
            children: [
              Text(c.tr('preview')),
              const SizedBox(height: 24),
              Field(controller: nick, label: c.tr('nickname')),
              Field(controller: bio, label: c.tr('bio'), lines: 3),
              Field(controller: prompt, label: c.tr('prompt'), lines: 2),
              Field(controller: interests, label: c.tr('interests')),
              Field(controller: gender, label: c.tr('gender')),
              Field(controller: preference, label: c.tr('preference')),
              BucketRange(value: radius, onChanged: (v) => radius = v),
              ToggleRow(
                label: c.tr('radarOpt'),
                value: radar,
                onChanged: (v) => setState(() => radar = v),
              ),
              Text(c.tr('privacy')),
              ToggleRow(
                label: c.tr('hideFriends'),
                value: hide,
                onChanged: (v) => setState(() => hide = v),
              ),
              FilledButton(
                onPressed: busy
                    ? null
                    : () async {
                        setState(() => busy = true);
                        await action(c, () async {
                          await ref
                              .read(apiProvider)
                              .request(
                                'PUT',
                                '/v1/dating-profile',
                                body: {
                                  'nickname': nick.text,
                                  'bio': bio.text,
                                  'photos': <String>[],
                                  'prompts': prompt.text
                                      .split('\n')
                                      .where((v) => v.trim().isNotEmpty)
                                      .toList(),
                                  'interests': split(interests.text),
                                  'gender': gender.text,
                                  'preferences': split(preference.text),
                                  'age_min': 18,
                                  'age_max': 99,
                                  'radar_opt_in': radar,
                                  'hide_from_social_friends': hide,
                                  'radius_km': radius,
                                },
                              );
                          if (c.mounted) {
                            notice(c, c.tr('saved'));
                            Navigator.pop(c);
                          }
                        });
                        if (mounted) setState(() => busy = false);
                      },
                child: Text(c.tr('save')),
              ),
            ],
          ),
  );
}

class SafetyCenter extends ConsumerStatefulWidget {
  const SafetyCenter({super.key});
  @override
  ConsumerState<SafetyCenter> createState() => _SafetyCenterState();
}

class _SafetyCenterState extends ConsumerState<SafetyCenter> {
  bool? ghost;
  int revision = 0;
  @override
  Widget build(BuildContext c) => PageFrame(
    title: c.tr('safety'),
    child: RemoteView(
      path: '/v1/profile',
      builder: (c, d, refresh) => ListView(
        padding: const EdgeInsets.all(24),
        children: [
          ToggleRow(
            label: c.tr('ghost'),
            value: ghost ?? d['ghost'] as bool,
            onChanged: (v) => action(c, () async {
              await ref
                  .read(apiProvider)
                  .request('POST', '/v1/safety/ghost', body: {'enabled': v});
              if (mounted) setState(() => ghost = v);
            }),
          ),
          Text(c.tr('whisperDisclosure')),
          const SizedBox(height: 24),
          Text(c.tr('reports'), style: Theme.of(c).textTheme.titleLarge),
          SizedBox(
            height: 220,
            child: RemoteView(
              path: '/v1/safety/reports',
              builder: (c, data, refresh) => ListView(
                children: [
                  for (final v in items(data))
                    ListTile(
                      title: Text(v['category'] as String),
                      subtitle: Text('${v['status']} · ${v['severity']}'),
                    ),
                ],
              ),
            ),
          ),
          Text(c.tr('blocks'), style: Theme.of(c).textTheme.titleLarge),
          SizedBox(
            height: 220,
            child: RemoteView(
              key: ValueKey(revision),
              path: '/v1/safety/blocks',
              builder: (c, data, refresh) => ListView(
                children: [
                  for (final v in items(data))
                    ListTile(
                      title: Text(v['target_id'] as String),
                      trailing: TextButton(
                        onPressed: () => action(c, () async {
                          await ref
                              .read(apiProvider)
                              .request(
                                'DELETE',
                                '/v1/safety/blocks/${v['target_id']}',
                              );
                          if (mounted) setState(() => revision++);
                        }),
                        child: Text(c.tr('unblock')),
                      ),
                    ),
                ],
              ),
            ),
          ),
        ],
      ),
    ),
  );
}

class Inbox extends StatelessWidget {
  const Inbox({super.key});
  @override
  Widget build(BuildContext c) => PageFrame(
    title: c.tr('notifications'),
    child: RemoteView(
      path: '/v1/notifications',
      builder: (c, d, refresh) => ListView(
        children: [
          if (items(d).isEmpty) EmptyState(c.tr('empty')),
          for (final v in items(d))
            ListTile(
              leading: Icon(
                v['category'] == 'SAFETY'
                    ? Icons.shield_outlined
                    : Icons.notifications_none,
              ),
              title: Text(v['kind'] as String),
              subtitle: Text(v['category'] as String),
            ),
        ],
      ),
    ),
  );
}

class SessionsPage extends ConsumerWidget {
  const SessionsPage({super.key});
  @override
  Widget build(BuildContext c, WidgetRef ref) => PageFrame(
    title: c.tr('sessions'),
    child: RemoteView(
      path: '/v1/auth/sessions',
      builder: (c, d, refresh) => ListView(
        children: [
          for (final v in items(d))
            ListTile(
              title: Text(v['device'] as String),
              subtitle: Text(v['expires_at'] as String),
              trailing: IconButton(
                tooltip: c.tr('delete'),
                onPressed: () => action(c, () async {
                  await ref
                      .read(apiProvider)
                      .request('DELETE', '/v1/auth/sessions/${v['id']}');
                  if (v['current'] == true) {
                    await ref.read(apiProvider).logout();
                    if (c.mounted) Navigator.of(c).popUntil((r) => r.isFirst);
                  } else {
                    refresh();
                  }
                }),
                icon: const Icon(Icons.close),
              ),
            ),
        ],
      ),
    ),
  );
}

class DeletePage extends ConsumerStatefulWidget {
  const DeletePage({super.key});
  @override
  ConsumerState<DeletePage> createState() => _DeletePageState();
}

class _DeletePageState extends ConsumerState<DeletePage> {
  final input = TextEditingController();
  @override
  void dispose() {
    input.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext c) => PageFrame(
    title: c.tr('delete'),
    child: ListView(
      padding: const EdgeInsets.all(24),
      children: [
        Text(c.tr('deleteWarning')),
        const SizedBox(height: 24),
        TextField(controller: input, onChanged: (_) => setState(() {})),
        const SizedBox(height: 24),
        FilledButton(
          onPressed: input.text != 'DELETE MY ACCOUNT'
              ? null
              : () => action(c, () async {
                  await ref
                      .read(apiProvider)
                      .request(
                        'DELETE',
                        '/v1/account',
                        body: {'confirm': input.text},
                      );
                  await ref.read(apiProvider).logout();
                  if (c.mounted) Navigator.of(c).popUntil((r) => r.isFirst);
                }),
          child: Text(c.tr('delete')),
        ),
      ],
    ),
  );
}

// Platform capture bridge is metadata-only. iOS capture signals are best-effort, not prevention.
const safetyChannel = MethodChannel('app.nexus/safety');
