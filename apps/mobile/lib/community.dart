import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'api.dart';
import 'l10n.dart';
import 'ui.dart';

class Community extends StatefulWidget {
  const Community({super.key});
  @override
  State<Community> createState() => _CommunityState();
}

class _CommunityState extends State<Community> {
  String tab = 'feed';
  @override
  Widget build(BuildContext c) => Column(
    children: [
      Padding(
        padding: const EdgeInsets.all(16),
        child: SearchBar(
          hintText: c.tr('search'),
          leading: const Icon(Icons.search),
          onTap: () => open(c, const SearchPage()),
          readOnly: true,
        ),
      ),
      SegmentedButton<String>(
        segments: [
          'feed',
          'events',
          'groups',
        ].map((v) => ButtonSegment(value: v, label: Text(c.tr(v)))).toList(),
        selected: {tab},
        onSelectionChanged: (v) => setState(() => tab = v.first),
      ),
      const SizedBox(height: 12),
      Expanded(
        child: RemoteView(
          path: tab == 'feed' ? '/v1/feed' : '/v1/$tab',
          builder: (c, d, refresh) {
            final list = items(d);
            return RefreshIndicator(
              onRefresh: () async {
                refresh();
              },
              child: ListView(
                padding: const EdgeInsets.all(16),
                children: [
                  if (tab == 'feed')
                    Text(
                      c.tr('highlights'),
                      style: Theme.of(c).textTheme.titleMedium,
                    ),
                  if (list.isEmpty) EmptyState(c.tr('empty')),
                  for (final v in list)
                    if (tab == 'feed')
                      PostCard(data: v)
                    else if (tab == 'events')
                      EventCard(data: v, onChanged: refresh)
                    else
                      GroupCard(data: v, onChanged: refresh),
                ],
              ),
            );
          },
        ),
      ),
    ],
  );
}

class PostCard extends ConsumerStatefulWidget {
  final Data data;
  const PostCard({super.key, required this.data});
  @override
  ConsumerState<PostCard> createState() => _PostCardState();
}

class _PostCardState extends ConsumerState<PostCard> {
  String? reaction;
  late int count;
  bool pending = false;
  @override
  void initState() {
    super.initState();
    reaction = widget.data['my_reaction'] as String?;
    count = (widget.data['reaction_count'] as num).toInt();
  }

  Future<void> react(String type) async {
    if (pending) return;
    final old = reaction, oldCount = count;
    setState(() {
      pending = true;
      reaction = type == 'NONE' ? null : type;
      count = oldCount + (old == null ? 1 : 0) - (type == 'NONE' ? 1 : 0);
    });
    try {
      await ref
          .read(apiProvider)
          .request(
            'POST',
            '/v1/posts/${widget.data['id']}/reactions',
            body: {'type': type},
          );
    } catch (_) {
      if (mounted) {
        setState(() {
          reaction = old;
          count = oldCount;
        });
        notice(context, context.tr('error'));
      }
    } finally {
      if (mounted) setState(() => pending = false);
    }
  }

  @override
  Widget build(BuildContext c) {
    final d = widget.data;
    return Panel(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Avatar(label: d['author'] as String),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      d['author'] as String,
                      style: const TextStyle(fontWeight: FontWeight.bold),
                    ),
                    Text(
                      '${d['township']} · ${d['audience']}',
                      style: Theme.of(c).textTheme.bodySmall,
                    ),
                  ],
                ),
              ),
              IconButton(
                tooltip: c.tr('safety'),
                onPressed: () =>
                    SafetySheet.show(c, ref, user: d['author_id'] as String),
                icon: const Icon(Icons.more_horiz),
              ),
            ],
          ),
          const SizedBox(height: 16),
          GestureDetector(
            onDoubleTap: () => react('LOVE'),
            child: Text(d['body'] as String),
          ),
          if (d['status'] == 'UNDER_REVIEW')
            Chip(label: Text(c.tr('underReview'))),
          const SizedBox(height: 16),
          Text('♡ $count    ◌ ${d['comment_count']}    ↗ ${d['share_count']}'),
          const Divider(),
          ReactionBar(
            selected: reaction,
            onSelected: react,
            onComment: () => open(c, CommentsPage(postId: d['id'] as String)),
          ),
          Row(
            mainAxisAlignment: MainAxisAlignment.spaceBetween,
            children: [
              TextButton.icon(
                onPressed: () => action(c, () async {
                  await ref
                      .read(apiProvider)
                      .request(
                        'POST',
                        '/v1/posts/${d['id']}/shares',
                        body: {'mode': 'COPY_LINK'},
                      );
                  if (c.mounted) notice(c, c.tr('done'));
                }),
                icon: const Icon(Icons.share_outlined),
                label: Text(c.tr('share')),
              ),
              IconButton(
                tooltip: c.tr('bookmark'),
                onPressed: () => action(c, () async {
                  await ref
                      .read(apiProvider)
                      .request(
                        'POST',
                        '/v1/posts/${d['id']}/bookmark',
                        body: {'saved': true},
                      );
                  if (c.mounted) notice(c, c.tr('saved'));
                }),
                icon: const Icon(Icons.bookmark_border),
              ),
            ],
          ),
        ],
      ),
    );
  }
}

class ReactionBar extends StatelessWidget {
  final String? selected;
  final ValueChanged<String> onSelected;
  final VoidCallback onComment;
  const ReactionBar({
    super.key,
    required this.selected,
    required this.onSelected,
    required this.onComment,
  });
  @override
  Widget build(BuildContext c) => Row(
    children: [
      Expanded(
        child: PopupMenuButton<String>(
          tooltip: c.tr('like'),
          onSelected: onSelected,
          itemBuilder: (c) => [
            'LIKE',
            'LOVE',
            'HAHA',
            'WOW',
            'SAD',
            'ANGRY',
            'NONE',
          ].map((v) => PopupMenuItem(value: v, child: Text(v))).toList(),
          child: Padding(
            padding: const EdgeInsets.all(12),
            child: Text(
              selected ?? c.tr('like'),
              style: const TextStyle(color: primary),
            ),
          ),
        ),
      ),
      Expanded(
        child: TextButton.icon(
          onPressed: onComment,
          icon: const Icon(Icons.chat_bubble_outline),
          label: Text(c.tr('comment')),
        ),
      ),
    ],
  );
}

class ComposePost extends ConsumerStatefulWidget {
  const ComposePost({super.key});
  @override
  ConsumerState<ComposePost> createState() => _ComposePostState();
}

class _ComposePostState extends ConsumerState<ComposePost> {
  final body = TextEditingController();
  String audience = 'PUBLIC', town = 'kamayut';
  bool busy = false;
  @override
  void dispose() {
    body.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext c) => PageFrame(
    title: c.tr('create'),
    child: ListView(
      padding: const EdgeInsets.all(24),
      children: [
        DropdownButtonFormField<String>(
          initialValue: audience,
          decoration: InputDecoration(labelText: c.tr('audience')),
          items: ['PUBLIC', 'COMMUNITY', 'FRIENDS']
              .map(
                (v) => DropdownMenuItem(
                  value: v,
                  child: Text(c.tr(v.toLowerCase())),
                ),
              )
              .toList(),
          onChanged: (v) => audience = v!,
        ),
        const SizedBox(height: 16),
        DropdownButtonFormField<String>(
          initialValue: town,
          decoration: InputDecoration(labelText: c.tr('township')),
          items: towns
              .map((v) => DropdownMenuItem(value: v, child: Text(v)))
              .toList(),
          onChanged: (v) => town = v!,
        ),
        const SizedBox(height: 24),
        Field(controller: body, label: c.tr('body'), lines: 7),
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
                          '/v1/posts',
                          body: {
                            'body': body.text,
                            'audience': audience,
                            'township': town,
                          },
                        );
                    if (c.mounted) {
                      notice(c, c.tr('underReview'));
                      Navigator.pop(c);
                    }
                  });
                  if (mounted) setState(() => busy = false);
                },
          child: Text(c.tr('publish')),
        ),
      ],
    ),
  );
}

class CommentsPage extends ConsumerStatefulWidget {
  final String postId;
  const CommentsPage({super.key, required this.postId});
  @override
  ConsumerState<CommentsPage> createState() => _CommentsPageState();
}

class _CommentsPageState extends ConsumerState<CommentsPage> {
  final body = TextEditingController();
  String? parent;
  int revision = 0;
  @override
  void dispose() {
    body.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext c) => PageFrame(
    title: c.tr('comment'),
    child: Column(
      children: [
        Expanded(
          child: RemoteView(
            key: ValueKey(revision),
            path: '/v1/posts/${widget.postId}/comments',
            builder: (c, d, refresh) => ListView(
              padding: const EdgeInsets.all(16),
              children: [
                for (final v in items(d))
                  Padding(
                    padding: EdgeInsets.only(
                      left: v['parent_comment_id'] == null ? 0 : 24,
                    ),
                    child: Panel(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            v['author'] as String,
                            style: const TextStyle(fontWeight: FontWeight.bold),
                          ),
                          Text(v['body'] as String),
                          if (v['status'] == 'UNDER_REVIEW')
                            Text(c.tr('underReview')),
                          if (v['parent_comment_id'] == null)
                            TextButton(
                              onPressed: () =>
                                  setState(() => parent = v['id'] as String),
                              child: const Icon(Icons.reply),
                            ),
                        ],
                      ),
                    ),
                  ),
              ],
            ),
          ),
        ),
        if (parent != null)
          TextButton(
            onPressed: () => setState(() => parent = null),
            child: Text(c.tr('cancel')),
          ),
        Padding(
          padding: const EdgeInsets.all(16),
          child: Row(
            children: [
              Expanded(
                child: TextField(
                  controller: body,
                  decoration: InputDecoration(hintText: c.tr('writeComment')),
                ),
              ),
              IconButton(
                tooltip: c.tr('send'),
                onPressed: () => action(c, () async {
                  await ref
                      .read(apiProvider)
                      .request(
                        'POST',
                        '/v1/posts/${widget.postId}/comments',
                        body: {
                          'body': body.text,
                          if (parent != null) 'parent_comment_id': parent,
                        },
                      );
                  body.clear();
                  if (mounted) {
                    setState(() {
                      parent = null;
                      revision++;
                    });
                  }
                }),
                icon: const Icon(Icons.send_outlined),
              ),
            ],
          ),
        ),
      ],
    ),
  );
}

class EventCard extends ConsumerWidget {
  final Data data;
  final VoidCallback onChanged;
  const EventCard({super.key, required this.data, required this.onChanged});
  @override
  Widget build(BuildContext c, WidgetRef ref) => Panel(
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const Icon(Icons.event, size: 48, color: primary),
        Text(data['title'] as String, style: Theme.of(c).textTheme.titleLarge),
        TownshipChip(data['township'] as String),
        Text(data['starts_at'] as String),
        Text(data['description'] as String),
        Text(data['venue_area'] as String),
        Row(
          children: ['ATTEND', 'NOT_GOING']
              .map(
                (v) => Expanded(
                  child: TextButton(
                    onPressed: () => action(c, () async {
                      await ref
                          .read(apiProvider)
                          .request(
                            'POST',
                            '/v1/events/${data['id']}/rsvp',
                            body: {'status': v},
                          );
                      onChanged();
                    }),
                    child: Text(c.tr(v == 'ATTEND' ? 'attend' : 'notGoing')),
                  ),
                ),
              )
              .toList(),
        ),
      ],
    ),
  );
}

class GroupCard extends ConsumerWidget {
  final Data data;
  final VoidCallback onChanged;
  const GroupCard({super.key, required this.data, required this.onChanged});
  @override
  Widget build(BuildContext c, WidgetRef ref) => Panel(
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(data['name'] as String, style: Theme.of(c).textTheme.titleLarge),
        Text(data['description'] as String),
        TownshipChip(data['township'] as String),
        TextButton(
          onPressed: () => action(c, () async {
            await ref
                .read(apiProvider)
                .request('POST', '/v1/groups/${data['id']}/join', body: {});
            onChanged();
          }),
          child: Text(c.tr(data['joined'] == true ? 'joined' : 'join')),
        ),
      ],
    ),
  );
}

class SearchPage extends ConsumerStatefulWidget {
  const SearchPage({super.key});
  @override
  ConsumerState<SearchPage> createState() => _SearchPageState();
}

class _SearchPageState extends ConsumerState<SearchPage> {
  final query = TextEditingController();
  String type = 'people', q = '';
  @override
  void dispose() {
    query.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext c) => PageFrame(
    title: c.tr('query'),
    child: Column(
      children: [
        Padding(
          padding: const EdgeInsets.all(16),
          child: TextField(
            controller: query,
            decoration: InputDecoration(
              hintText: c.tr('search'),
              suffixIcon: IconButton(
                tooltip: c.tr('query'),
                onPressed: () => setState(() => q = query.text.trim()),
                icon: const Icon(Icons.search),
              ),
            ),
            onSubmitted: (v) => setState(() => q = v.trim()),
          ),
        ),
        SegmentedButton<String>(
          segments: [
            'people',
            'posts',
            'events',
          ].map((v) => ButtonSegment(value: v, label: Text(c.tr(v)))).toList(),
          selected: {type},
          onSelectionChanged: (v) => setState(() => type = v.first),
        ),
        Expanded(
          child: q.length < 2
              ? EmptyState(c.tr('query'))
              : RemoteView(
                  path:
                      '/v1/search?type=$type&q=${Uri.encodeQueryComponent(q)}',
                  builder: (c, d, refresh) => ListView(
                    padding: const EdgeInsets.all(16),
                    children: [
                      for (final v in items(d))
                        Panel(
                          child: Text(
                            (v['real_name'] ?? v['body'] ?? v['title'] ?? '')
                                .toString(),
                          ),
                        ),
                      if (items(d).isEmpty) EmptyState(c.tr('empty')),
                    ],
                  ),
                ),
        ),
      ],
    ),
  );
}
