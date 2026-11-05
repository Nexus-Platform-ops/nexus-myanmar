import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'api.dart';
import 'l10n.dart';

const primary = Color(0xFF4C5FD5);
const ink = Color(0xFF1E2A56);
const towns = ['kamayut', 'bahan', 'chanayethazan', 'bangkok', 'chiang-mai'];
typedef Data = Map<String, dynamic>;
List<Data> items(Data data) => (data['items'] as List? ?? [])
    .map((e) => Map<String, dynamic>.from(e as Map))
    .toList();
void notice(BuildContext context, String text) =>
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(text)));
Future<void> action(BuildContext context, Future<void> Function() fn) async {
  try {
    await fn();
  } catch (_) {
    if (context.mounted) notice(context, context.tr('error'));
  }
}

Future<void> info(BuildContext c, String key) async {
  await showDialog<void>(
    context: c,
    builder: (c) => AlertDialog(
      content: Text(c.tr(key)),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(c),
          child: Text(c.tr('close')),
        ),
      ],
    ),
  );
}

Future<void> open(BuildContext c, Widget w) =>
    Navigator.of(c).push(MaterialPageRoute<void>(builder: (_) => w));

class Panel extends StatelessWidget {
  final Widget child;
  const Panel({super.key, required this.child});
  @override
  Widget build(BuildContext c) => Card(
    margin: const EdgeInsets.only(bottom: 16),
    child: Padding(padding: const EdgeInsets.all(16), child: child),
  );
}

class Avatar extends StatelessWidget {
  final String label;
  final int tier;
  const Avatar({super.key, required this.label, this.tier = 0});
  @override
  Widget build(BuildContext c) => Semantics(
    label: '$label $tier',
    child: CircleAvatar(
      backgroundColor: primary.withValues(alpha: .10),
      child: Text(
        label.isEmpty ? 'N' : label.characters.first,
        style: const TextStyle(color: primary),
      ),
    ),
  );
}

class TownshipChip extends StatelessWidget {
  final String township;
  const TownshipChip(this.township, {super.key});
  @override
  Widget build(BuildContext c) => Chip(
    avatar: const Icon(Icons.location_city, size: 16),
    label: Text(township),
  );
}

class DistanceBucketLabel extends StatelessWidget {
  final String bucket;
  const DistanceBucketLabel(this.bucket, {super.key});
  @override
  Widget build(BuildContext c) => Chip(
    avatar: const Icon(Icons.shield_outlined, size: 16),
    label: Text('≈ $bucket'),
  );
}

class EmptyState extends StatelessWidget {
  final String text;
  const EmptyState(this.text, {super.key});
  @override
  Widget build(BuildContext c) => Padding(
    padding: const EdgeInsets.all(32),
    child: Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        const Icon(Icons.hub_outlined, size: 56, color: primary),
        const SizedBox(height: 16),
        Text(text, textAlign: TextAlign.center),
      ],
    ),
  );
}

class ToggleRow extends StatelessWidget {
  final String label;
  final bool value;
  final ValueChanged<bool> onChanged;
  const ToggleRow({
    super.key,
    required this.label,
    required this.value,
    required this.onChanged,
  });
  @override
  Widget build(BuildContext c) => SwitchListTile(
    contentPadding: EdgeInsets.zero,
    title: Text(label),
    value: value,
    onChanged: onChanged,
  );
}

class BucketRange extends StatelessWidget {
  final int value;
  final ValueChanged<int> onChanged;
  const BucketRange({super.key, required this.value, required this.onChanged});
  @override
  Widget build(BuildContext c) => DropdownButtonFormField<int>(
    initialValue: value,
    decoration: InputDecoration(labelText: c.tr('radius')),
    items: [
      1,
      3,
      5,
      10,
    ].map((i) => DropdownMenuItem(value: i, child: Text('$i km'))).toList(),
    onChanged: (v) {
      if (v != null) onChanged(v);
    },
  );
}

class RemoteView extends ConsumerStatefulWidget {
  final String path;
  final Widget Function(BuildContext, Data, VoidCallback) builder;
  const RemoteView({super.key, required this.path, required this.builder});
  @override
  ConsumerState<RemoteView> createState() => _RemoteViewState();
}

class _RemoteViewState extends ConsumerState<RemoteView> {
  late Future<Data> future;
  @override
  void initState() {
    super.initState();
    load();
  }

  @override
  void didUpdateWidget(RemoteView old) {
    super.didUpdateWidget(old);
    if (old.path != widget.path) load();
  }

  void load() {
    future = ref.read(apiProvider).request('GET', widget.path);
  }

  void refresh() {
    setState(load);
  }

  @override
  Widget build(BuildContext c) => FutureBuilder<Data>(
    future: future,
    builder: (c, s) {
      if (s.connectionState != ConnectionState.done) {
        return const Center(child: CircularProgressIndicator());
      }
      if (s.hasError) {
        return Center(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Text(c.tr('error')),
              TextButton(onPressed: refresh, child: Text(c.tr('retry'))),
            ],
          ),
        );
      }
      return widget.builder(c, s.data!, refresh);
    },
  );
}

class PageFrame extends StatelessWidget {
  final String title;
  final Widget child;
  final List<Widget>? actions;
  const PageFrame({
    super.key,
    required this.title,
    required this.child,
    this.actions,
  });
  @override
  Widget build(BuildContext c) => Scaffold(
    appBar: AppBar(title: Text(title), actions: actions),
    body: SafeArea(
      child: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 640),
          child: child,
        ),
      ),
    ),
  );
}

class Field extends StatelessWidget {
  final TextEditingController controller;
  final String label;
  final int lines;
  final TextInputType? keyboard;
  const Field({
    super.key,
    required this.controller,
    required this.label,
    this.lines = 1,
    this.keyboard,
  });
  @override
  Widget build(BuildContext c) => Padding(
    padding: const EdgeInsets.only(bottom: 16),
    child: TextField(
      controller: controller,
      maxLines: lines,
      keyboardType: keyboard,
      decoration: InputDecoration(labelText: label),
    ),
  );
}

class SafetySheet {
  static Future<void> show(
    BuildContext context,
    WidgetRef ref, {
    String? user,
    String? profile,
  }) async {
    final target = <String, dynamic>{
      'target_id': ?user,
      'profile_id': ?profile,
    };
    await showModalBottomSheet<void>(
      context: context,
      builder: (c) => SafeArea(
        child: Wrap(
          children: [
            ListTile(
              leading: const Icon(Icons.block),
              title: Text(c.tr('block')),
              onTap: () async {
                Navigator.pop(c);
                await action(context, () async {
                  await ref
                      .read(apiProvider)
                      .request('POST', '/v1/safety/block', body: target);
                  if (context.mounted) notice(context, context.tr('done'));
                });
              },
            ),
            ListTile(
              leading: const Icon(Icons.flag_outlined),
              title: Text(c.tr('report')),
              onTap: () {
                Navigator.pop(c);
                open(context, ReportPage(target: target));
              },
            ),
          ],
        ),
      ),
    );
  }
}

class ReportPage extends ConsumerStatefulWidget {
  final Data target;
  const ReportPage({super.key, required this.target});
  @override
  ConsumerState<ReportPage> createState() => _ReportPageState();
}

class _ReportPageState extends ConsumerState<ReportPage> {
  final details = TextEditingController();
  String category = 'HARASSMENT';
  bool busy = false;
  @override
  void dispose() {
    details.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext c) => PageFrame(
    title: c.tr('report'),
    child: ListView(
      padding: const EdgeInsets.all(24),
      children: [
        DropdownButtonFormField<String>(
          initialValue: category,
          items: [
            'SCAM',
            'HARASSMENT',
            'UNDERAGE',
            'VIOLENCE',
            'OTHER',
          ].map((v) => DropdownMenuItem(value: v, child: Text(v))).toList(),
          onChanged: (v) => setState(() => category = v!),
        ),
        const SizedBox(height: 16),
        Field(controller: details, label: c.tr('details'), lines: 5),
        FilledButton(
          onPressed: busy
              ? null
              : () async {
                  setState(() => busy = true);
                  await action(c, () async {
                    await ref
                        .read(apiProvider)
                        .request(
                          'POST',
                          '/v1/safety/report',
                          body: {
                            ...widget.target,
                            'category': category,
                            'details': details.text,
                          },
                        );
                    if (c.mounted) {
                      notice(c, c.tr('done'));
                      Navigator.pop(c);
                    }
                  });
                  if (mounted) setState(() => busy = false);
                },
          child: Text(c.tr('report')),
        ),
      ],
    ),
  );
}
