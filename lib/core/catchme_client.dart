/// Talks to the daemon over its WebSocket (protocol v1) and keeps the list of
/// downloads up to date for the UI.
library;

import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:logging/logging.dart';

import 'download.dart';

enum LinkState { connecting, connected, disconnected }

/// An error the daemon answered to a command, e.g. code "bad_url".
class CommandException implements Exception {
  CommandException(this.code, this.message);
  final String code;
  final String message;
  @override
  String toString() => message;
}

class CatchMeClient extends ChangeNotifier {
  CatchMeClient({required this.address, required this.token});

  static const protocol = 1;
  static const _closeLagged = 4000;

  final String address;
  final String token;
  final _log = Logger('CatchMeClient');

  LinkState state = LinkState.disconnected;
  String? daemonVersion;

  final Map<String, Download> _byId = {};
  final List<String> _order = [];
  WebSocket? _ws;
  int _nextId = 0;
  final Map<String, Completer<Map<String, dynamic>>> _pending = {};
  bool _closed = false;
  Duration _retry = const Duration(milliseconds: 250);

  /// Every download, in the order it was added.
  List<Download> get downloads => [for (final id in _order) _byId[id]!];

  /// Connects and keeps reconnecting until [close]. Completes after the first
  /// successful connection.
  Future<void> connect() async {
    final first = Completer<void>();
    unawaited(_run(first));
    return first.future;
  }

  Future<void> _run(Completer<void> first) async {
    while (!_closed) {
      _setState(LinkState.connecting);
      try {
        final ws = await WebSocket.connect(
          'ws://$address/v1/ws',
          headers: {'Authorization': 'Bearer $token'},
        ).timeout(const Duration(seconds: 5));
        _ws = ws;
        _retry = const Duration(milliseconds: 250);
        await for (final data in ws) {
          if (data is String) _handle(jsonDecode(data) as Map<String, dynamic>, first);
        }
        if (ws.closeCode == _closeLagged) _log.info('fell behind on events; reloading the list');
      } catch (e) {
        _log.warning('connection failed: $e');
      }
      _ws = null;
      _failPending('Lost the connection to the download engine.');
      if (_closed) break;
      _setState(LinkState.disconnected);
      await Future<void>.delayed(_retry);
      _retry = _retry * 2 > const Duration(seconds: 5) ? const Duration(seconds: 5) : _retry * 2;
    }
  }

  void _handle(Map<String, dynamic> msg, Completer<void> first) {
    switch (msg['type']) {
      case 'hello':
        if (msg['protocol'] != protocol) {
          _log.severe('daemon speaks protocol ${msg['protocol']}, app expects $protocol');
        }
        daemonVersion = msg['version'] as String?;
        _byId.clear();
        _order.clear();
        for (final j in (msg['downloads'] as List? ?? const [])) {
          final d = Download.fromJson(j as Map<String, dynamic>);
          _byId[d.id] = d;
          _order.add(d.id);
        }
        state = LinkState.connected;
        if (!first.isCompleted) first.complete();
        notifyListeners();
      case 'event':
        final ev = msg['event'] as Map<String, dynamic>;
        final d = Download.fromJson(ev['download'] as Map<String, dynamic>);
        if (ev['kind'] == 'removed') {
          _byId.remove(d.id);
          _order.remove(d.id);
        } else {
          if (!_byId.containsKey(d.id)) _order.add(d.id);
          _byId[d.id] = d;
        }
        notifyListeners();
      case 'reply':
        _pending.remove(msg['id'])?.complete(msg);
    }
  }

  Future<String?> _send(Map<String, dynamic> cmd) async {
    final ws = _ws;
    if (ws == null || state != LinkState.connected) {
      throw CommandException('offline', 'Not connected to the download engine.');
    }
    final id = '${++_nextId}';
    final reply = Completer<Map<String, dynamic>>();
    _pending[id] = reply;
    ws.add(jsonEncode({...cmd, 'id': id}));
    final r = await reply.future.timeout(
      const Duration(seconds: 10),
      onTimeout: () {
        _pending.remove(id);
        throw CommandException('timeout', 'The download engine did not answer.');
      },
    );
    if (r['ok'] != true) {
      final err = r['error'] as Map<String, dynamic>? ?? const {};
      throw CommandException(err['code'] as String? ?? 'internal', err['message'] as String? ?? 'Unknown error');
    }
    return r['downloadId'] as String?;
  }

  Future<String> add(String url) async => (await _send({'type': 'add', 'url': url.trim()}))!;
  Future<void> pause(String id) => _send({'type': 'pause', 'downloadId': id});
  Future<void> resume(String id) => _send({'type': 'resume', 'downloadId': id});
  Future<void> cancel(String id) => _send({'type': 'cancel', 'downloadId': id});
  Future<void> remove(String id) => _send({'type': 'remove', 'downloadId': id});

  void _failPending(String why) {
    for (final c in _pending.values) {
      c.completeError(CommandException('offline', why));
    }
    _pending.clear();
  }

  void _setState(LinkState s) {
    if (state == s) return;
    state = s;
    notifyListeners();
  }

  Future<void> close() async {
    _closed = true;
    await _ws?.close();
  }
}
