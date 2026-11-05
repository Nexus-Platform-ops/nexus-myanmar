import 'dart:async';
import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:uuid/uuid.dart';
import 'package:web_socket_channel/web_socket_channel.dart';
import 'api.dart';
import 'l10n.dart';
import 'ui.dart';

class ChatList extends StatelessWidget {
  const ChatList({super.key});
  @override
  Widget build(BuildContext c) => RemoteView(
    path: '/v1/chats',
    builder: (c, d, refresh) => RefreshIndicator(
      onRefresh: () async {
        refresh();
      },
      child: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          TextButton.icon(
            onPressed: () => info(c, 'unavailable'),
            icon: const Icon(Icons.call_outlined),
            label: Text(c.tr('calls')),
          ),
          if (items(d).isEmpty) EmptyState(c.tr('empty')),
          for (final v in items(d))
            Panel(
              child: ListTile(
                leading: const CircleAvatar(
                  child: Icon(Icons.chat_bubble_outline),
                ),
                title: Text(v['kind'] as String),
                subtitle: Text((v['id'] as String).substring(0, 8)),
                trailing: const Icon(Icons.chevron_right),
                onTap: () => open(c, ChatPage(room: v['id'] as String)),
              ),
            ),
        ],
      ),
    ),
  );
}

class ChatPage extends ConsumerStatefulWidget {
  final String room;
  const ChatPage({super.key, required this.room});
  @override
  ConsumerState<ChatPage> createState() => _ChatPageState();
}

class _ChatPageState extends ConsumerState<ChatPage>
    with WidgetsBindingObserver {
  final text = TextEditingController();
  bool whisper = false, busy = false;
  List<Data> messages = [];
  bool loading = true;
  String? error;
  WebSocketChannel? socket;
  StreamSubscription<dynamic>? subscription;
  Timer? retry;
  bool active = true;
  int backoff = 1;
  final expired = <String>{};
  final revealing = <String>{};
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    load();
    connect();
    const MethodChannel('app.nexus/safety').setMethodCallHandler((call) async {
      if (call.method == 'capture' && mounted) {
        await action(context, () async {
          await ref
              .read(apiProvider)
              .request(
                'POST',
                '/v1/safety/capture-alert',
                body: {'room_id': widget.room, 'kind': 'SCREENSHOT'},
              );
        });
      }
    });
  }

  Future<void> load() async {
    try {
      final d = await ref
          .read(apiProvider)
          .request('GET', '/v1/chats/${widget.room}/messages');
      if (mounted && active) {
        setState(() {
          messages = items(d);
          loading = false;
          error = null;
        });
      }
    } catch (_) {
      if (mounted) {
        setState(() {
          loading = false;
          error = 'error';
        });
      }
    }
  }

  Future<void> connect() async {
    if (!mounted || !active) return;
    try {
      final api = ref.read(apiProvider);
      final d = await api.request('POST', '/v1/realtime/ticket', body: {});
      if (!mounted || !active) return;
      final uri = Uri.parse(api.base).replace(
        scheme: api.base.startsWith('https') ? 'wss' : 'ws',
        path: '/v1/ws',
        queryParameters: {'ticket': d['ticket'] as String},
      );
      socket = WebSocketChannel.connect(uri);
      await socket!.ready;
      if (!mounted || !active) {
        await socket?.sink.close();
        return;
      }
      backoff = 1;
      subscription = socket!.stream.listen(
        (raw) {
          final d = jsonDecode(raw as String) as Data;
          if (d['type'] == 'invalidate') load();
        },
        onError: (_) => scheduleReconnect(),
        onDone: scheduleReconnect,
      );
    } catch (_) {
      scheduleReconnect();
    }
  }

  void scheduleReconnect() {
    if (!mounted || !active || retry?.isActive == true) return;
    retry = Timer(Duration(seconds: backoff), () {
      connect();
      load();
    });
    backoff = (backoff * 2).clamp(1, 30);
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    active = state == AppLifecycleState.resumed;
    if (!active) {
      retry?.cancel();
      subscription?.cancel();
      socket?.sink.close();
      if (mounted) setState(() => messages = []);
    } else {
      load();
      connect();
    }
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    retry?.cancel();
    subscription?.cancel();
    socket?.sink.close();
    const MethodChannel('app.nexus/safety').setMethodCallHandler(null);
    text.clear();
    text.dispose();
    super.dispose();
  }

  Future<void> reveal(Data m) async {
    final id = m['id'] as String;
    if (expired.contains(id)) return;
    setState(() => revealing.add(id));
    await action(context, () async {
      await ref
          .read(apiProvider)
          .request(
            'POST',
            '/v1/chats/${widget.room}/read',
            body: {'through_seq': m['seq']},
          );
    });
    await Future<void>.delayed(const Duration(seconds: 1));
    if (mounted) {
      setState(() {
        revealing.remove(id);
        expired.add(id);
        messages.removeWhere((x) => x['id'] == id);
      });
    }
  }

  @override
  Widget build(BuildContext c) => PageFrame(
    title: c.tr('chats'),
    actions: [
      IconButton(
        tooltip: c.tr('refresh'),
        onPressed: load,
        icon: const Icon(Icons.refresh),
      ),
      IconButton(
        tooltip: c.tr('calls'),
        onPressed: () => info(c, 'unavailable'),
        icon: const Icon(Icons.video_call_outlined),
      ),
    ],
    child: Column(
      children: [
        Padding(
          padding: const EdgeInsets.all(12),
          child: Text(
            c.tr('whisperDisclosure'),
            style: Theme.of(c).textTheme.bodySmall,
          ),
        ),
        if (loading) const LinearProgressIndicator(),
        if (error != null) Text(c.tr(error!)),
        Expanded(
          child: ListView(
            padding: const EdgeInsets.all(16),
            children: [
              for (final m in messages)
                Align(
                  alignment: m['sender'] == ref.read(apiProvider).userId
                      ? Alignment.centerRight
                      : Alignment.centerLeft,
                  child: ConstrainedBox(
                    constraints: const BoxConstraints(maxWidth: 350),
                    child: Panel(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          if (m['mode'] == 'WHISPER')
                            const Icon(
                              Icons.hourglass_empty,
                              size: 16,
                              color: primary,
                            ),
                          if (m['mode'] == 'WHISPER' &&
                              m['sender'] != ref.read(apiProvider).userId &&
                              !revealing.contains(m['id']))
                            TextButton(
                              onPressed: () => reveal(m),
                              child: Text(c.tr('reveal')),
                            )
                          else
                            Text(m['text'] as String),
                          if (m['sender'] != ref.read(apiProvider).userId)
                            Align(
                              alignment: Alignment.centerRight,
                              child: IconButton(
                                tooltip: c.tr('safety'),
                                onPressed: () => SafetySheet.show(
                                  c,
                                  ref,
                                  user: m['sender'] as String,
                                ),
                                icon: const Icon(Icons.more_horiz, size: 18),
                              ),
                            ),
                        ],
                      ),
                    ),
                  ),
                ),
            ],
          ),
        ),
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: 16),
          child: ToggleRow(
            label: c.tr('whisper'),
            value: whisper,
            onChanged: (v) async {
              if (v) await info(c, 'whisperDisclosure');
              if (mounted) setState(() => whisper = v);
            },
          ),
        ),
        Padding(
          padding: const EdgeInsets.all(16),
          child: Row(
            children: [
              Expanded(
                child: TextField(
                  controller: text,
                  maxLines: null,
                  decoration: InputDecoration(hintText: c.tr('message')),
                ),
              ),
              IconButton(
                tooltip: c.tr('send'),
                onPressed: busy
                    ? null
                    : () async {
                        setState(() => busy = true);
                        await action(c, () async {
                          await ref
                              .read(apiProvider)
                              .request(
                                'POST',
                                '/v1/chats/${widget.room}/messages',
                                body: {
                                  'text': text.text,
                                  'mode': whisper ? 'WHISPER' : 'NORMAL',
                                  'client_id': const Uuid().v4(),
                                },
                              );
                          text.clear();
                          await load();
                        });
                        if (mounted) setState(() => busy = false);
                      },
                icon: const Icon(Icons.send_outlined),
              ),
            ],
          ),
        ),
      ],
    ),
  );
}
