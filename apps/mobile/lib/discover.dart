import 'dart:math' as math;
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'api.dart';
import 'l10n.dart';
import 'ui.dart';

class Discover extends ConsumerStatefulWidget {
  const Discover({super.key});
  @override
  ConsumerState<Discover> createState() => _DiscoverState();
}

class _DiscoverState extends ConsumerState<Discover> {
  String mode = 'cards';
  int revision = 0;
  @override
  Widget build(BuildContext c) => Column(
    children: [
      Padding(
        padding: const EdgeInsets.all(16),
        child: SegmentedButton<String>(
          segments: [
            'cards',
            'nearby',
            'radar',
          ].map((v) => ButtonSegment(value: v, label: Text(c.tr(v)))).toList(),
          selected: {mode},
          onSelectionChanged: (v) => setState(() => mode = v.first),
        ),
      ),
      Padding(
        padding: const EdgeInsets.symmetric(horizontal: 16),
        child: Text(c.tr('privacy'), style: Theme.of(c).textTheme.bodySmall),
      ),
      TextButton.icon(
        onPressed: () async {
          await info(c, 'permission');
          if (!c.mounted) return;
          await action(c, () async {
            final d = await ref.read(apiProvider).request('GET', '/v1/profile');
            await ref.read(apiProvider).updateLocation(d['township'] as String);
            if (mounted) setState(() => revision++);
          });
        },
        icon: const Icon(Icons.my_location),
        label: Text(c.tr('location')),
      ),
      Expanded(
        child: RemoteView(
          key: ValueKey('$mode$revision'),
          path: mode == 'radar'
              ? '/v1/radar'
              : mode == 'nearby'
              ? '/v1/lookaround?band=500m'
              : '/v1/discover',
          builder: (c, d, refresh) {
            final list = items(d);
            if (list.isEmpty) {
              return RefreshIndicator(
                onRefresh: () async {
                  refresh();
                },
                child: ListView(children: [EmptyState(c.tr('quiet'))]),
              );
            }
            if (mode == 'radar') {
              return ListView(
                padding: const EdgeInsets.all(16),
                children: [
                  RadarView(
                    blips: list,
                    onTap: (blip) => action(c, () async {
                      final card = await ref
                          .read(apiProvider)
                          .request('GET', '/v1/radar/blips/${blip['blip_id']}');
                      if (c.mounted) {
                        await showModalBottomSheet<void>(
                          context: c,
                          isScrollControlled: true,
                          builder: (c) => SingleChildScrollView(
                            child: Padding(
                              padding: const EdgeInsets.all(16),
                              child: ProfileCard(
                                data: card,
                                onChanged: refresh,
                              ),
                            ),
                          ),
                        );
                      }
                    }),
                  ),
                  Text(c.tr('privacy')),
                ],
              );
            }
            return RefreshIndicator(
              onRefresh: () async {
                refresh();
              },
              child: ListView(
                padding: const EdgeInsets.all(16),
                children: list
                    .map((v) => ProfileCard(data: v, onChanged: refresh))
                    .toList(),
              ),
            );
          },
        ),
      ),
    ],
  );
}

class ProfileCard extends ConsumerStatefulWidget {
  final Data data;
  final VoidCallback onChanged;
  const ProfileCard({super.key, required this.data, required this.onChanged});
  @override
  ConsumerState<ProfileCard> createState() => _ProfileCardState();
}

class _ProfileCardState extends ConsumerState<ProfileCard> {
  bool busy = false;
  @override
  Widget build(BuildContext c) {
    final d = widget.data;
    return GestureDetector(
      onLongPress: () =>
          SafetySheet.show(c, ref, profile: d['profile_id'] as String),
      child: Panel(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Container(
              height: 180,
              width: double.infinity,
              decoration: BoxDecoration(
                borderRadius: BorderRadius.circular(12),
                gradient: const LinearGradient(
                  colors: [Color(0xFFE9ECFF), Color(0xFFD9F7FF)],
                ),
              ),
              child: const Icon(Icons.person_outline, size: 92, color: primary),
            ),
            const SizedBox(height: 16),
            Row(
              children: [
                Expanded(
                  child: Text(
                    '${d['nickname']}, ${d['age']}',
                    style: const TextStyle(
                      fontSize: 24,
                      fontWeight: FontWeight.bold,
                    ),
                  ),
                ),
                IconButton(
                  tooltip: c.tr('safety'),
                  onPressed: () => SafetySheet.show(
                    c,
                    ref,
                    profile: d['profile_id'] as String,
                  ),
                  icon: const Icon(Icons.shield_outlined),
                ),
              ],
            ),
            Wrap(
              spacing: 8,
              children: [
                DistanceBucketLabel(d['distance_bucket'] as String? ?? '—'),
                TownshipChip(d['township'] as String),
              ],
            ),
            Text(d['bio'] as String? ?? ''),
            Wrap(
              spacing: 6,
              children: (d['interests'] as List? ?? [])
                  .map((v) => Chip(label: Text('$v')))
                  .toList(),
            ),
            for (final p in d['prompts'] as List? ?? []) Text('$p'),
            const SizedBox(height: 12),
            Row(
              children: ['PASS', 'LIKE']
                  .map(
                    (intent) => Expanded(
                      child: Padding(
                        padding: const EdgeInsets.all(4),
                        child: FilledButton.tonalIcon(
                          onPressed: busy
                              ? null
                              : () async {
                                  setState(() => busy = true);
                                  await action(c, () async {
                                    final data = await ref
                                        .read(apiProvider)
                                        .request(
                                          'POST',
                                          '/v1/actions/swipe',
                                          body: {
                                            'profile_id': d['profile_id'],
                                            'intent': intent,
                                          },
                                        );
                                    if (c.mounted && data['matched'] == true) {
                                      notice(c, c.tr('match'));
                                    }
                                    widget.onChanged();
                                  });
                                  if (mounted) setState(() => busy = false);
                                },
                          icon: Icon(
                            intent == 'LIKE'
                                ? Icons.favorite_border
                                : Icons.close,
                          ),
                          label: Text(c.tr(intent == 'LIKE' ? 'like' : 'pass')),
                        ),
                      ),
                    ),
                  )
                  .toList(),
            ),
          ],
        ),
      ),
    );
  }
}

class RadarView extends StatefulWidget {
  final List<Data> blips;
  final ValueChanged<Data> onTap;
  const RadarView({super.key, required this.blips, required this.onTap});
  @override
  State<RadarView> createState() => _RadarViewState();
}

class _RadarViewState extends State<RadarView>
    with SingleTickerProviderStateMixin {
  late AnimationController controller;
  @override
  void initState() {
    super.initState();
    controller = AnimationController(
      vsync: this,
      duration: const Duration(milliseconds: 2500),
    );
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    if (MediaQuery.disableAnimationsOf(context)) {
      controller.stop();
    } else {
      controller.repeat();
    }
  }

  @override
  void dispose() {
    controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext c) => AspectRatio(
    aspectRatio: 1,
    child: LayoutBuilder(
      builder: (c, b) {
        final size = b.maxWidth;
        return Stack(
          alignment: Alignment.center,
          children: [
            Positioned.fill(
              child: AnimatedBuilder(
                animation: controller,
                builder: (c, _) =>
                    CustomPaint(painter: _RadarPainter(controller.value)),
              ),
            ),
            const CircleAvatar(child: Icon(Icons.person_outline)),
            for (final d in widget.blips)
              Positioned(
                left:
                    size / 2 +
                    math.cos(
                          (d['display_angle'] as num).toDouble() *
                              math.pi /
                              180,
                        ) *
                        size *
                        .13 *
                        (d['display_ring'] as num) -
                    22,
                top:
                    size / 2 +
                    math.sin(
                          (d['display_angle'] as num).toDouble() *
                              math.pi /
                              180,
                        ) *
                        size *
                        .13 *
                        (d['display_ring'] as num) -
                    22,
                child: Blip(
                  label: '${c.tr('nearby')} ≈ ${d['distance_bucket']}',
                  onTap: () => widget.onTap(d),
                ),
              ),
          ],
        );
      },
    ),
  );
}

class Blip extends StatelessWidget {
  final String label;
  final VoidCallback onTap;
  const Blip({super.key, required this.label, required this.onTap});
  @override
  Widget build(BuildContext c) => Semantics(
    button: true,
    label: label,
    child: IconButton(
      tooltip: label,
      onPressed: onTap,
      icon: const Icon(Icons.circle, color: Color(0xFF35C4E8), size: 14),
    ),
  );
}

class _RadarPainter extends CustomPainter {
  final double progress;
  _RadarPainter(this.progress);
  @override
  void paint(Canvas canvas, Size size) {
    final center = size.center(Offset.zero);
    final p = Paint()
      ..color = const Color(0xFF35C4E8).withValues(alpha: .3)
      ..style = PaintingStyle.stroke
      ..strokeWidth = 1;
    for (final r in [.13, .26, .39]) {
      canvas.drawCircle(center, size.width * r, p);
    }
    p.color = primary.withValues(alpha: .2);
    canvas.drawLine(
      center,
      center +
          Offset(
                math.cos(progress * math.pi * 2),
                math.sin(progress * math.pi * 2),
              ) *
              size.width *
              .43,
      p,
    );
  }

  @override
  bool shouldRepaint(_RadarPainter old) => old.progress != progress;
}
