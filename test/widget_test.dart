import 'dart:async';
import 'dart:io';

import 'package:catchme/core/catchme_client.dart';
import 'package:catchme/core/daemon.dart';
import 'package:catchme/core/download.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  group('Download', () {
    test('parses a snapshot from the daemon', () {
      final d = Download.fromJson({
        'id': 'abc',
        'url': 'https://example.com/files/Pel%C3%ADcula.mkv?token=1',
        'status': 'running',
        'done': 512,
        'total': 2048,
        'speed': 256.0,
        'addedAt': '2026-11-25T12:00:00Z',
      });
      expect(d.status, DownloadStatus.running);
      expect(d.fraction, 0.25);
      expect(d.displayName, 'Película.mkv'); // from the URL until the daemon names it
      expect(d.eta, const Duration(seconds: 6));
    });

    test('unknown size has no fraction or ETA', () {
      final d = Download.fromJson({'id': 'x', 'url': 'https://e/x', 'status': 'running', 'total': -1, 'speed': 10.0});
      expect(d.fraction, isNull);
      expect(d.eta, isNull);
    });

    test('formats sizes', () {
      expect(formatBytes(0), '0 B');
      expect(formatBytes(1536), '1.5 KB');
      expect(formatBytes(700 * 1024 * 1024), '700 MB');
      expect(formatDuration(const Duration(minutes: 2, seconds: 5)), '2 min 5 s');
    });
  });

  // Runs the real Go daemon. Build it first with `make build` in catchmed/.
  group('engine', () {
    String? binary;
    try {
      binary = Daemon.findBinary();
    } on DaemonException {
      binary = null;
    }

    test('starts, connects and downloads a file', () async {
      final files = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      final body = List<int>.generate(200000, (i) => i % 251);
      files.listen((req) {
        req.response.headers.contentLength = body.length;
        req.response.add(body);
        req.response.close();
      });

      final tmp = await Directory.systemTemp.createTemp('catchme_test');
      final daemon = Daemon(downloadDir: '${tmp.path}/files', stateDir: '${tmp.path}/state');
      final addr = await daemon.start();
      final client = CatchMeClient(address: addr, token: daemon.token);
      addTearDown(() async {
        await client.close();
        await daemon.stop();
        await files.close(force: true);
        await tmp.delete(recursive: true);
      });
      await client.connect();
      expect(client.state, LinkState.connected);

      final id = await client.add('http://127.0.0.1:${files.port}/data.bin');
      final done = Completer<Download>();
      void check() {
        final d = client.downloads.where((d) => d.id == id).firstOrNull;
        if (d?.status == DownloadStatus.completed && !done.isCompleted) done.complete(d);
      }

      client.addListener(check);
      check();
      final d = await done.future.timeout(const Duration(seconds: 15));
      expect(d.total, body.length);
      expect(await File(d.path!).length(), body.length);
      await File(d.path!).delete();
      await client.remove(id);

      await expectLater(
        client.add('ftp://nope'),
        throwsA(isA<CommandException>().having((e) => e.code, 'code', 'bad_url')),
      );
    }, skip: binary == null ? 'catchmed not built (run make build in catchmed/)' : false);
  });
}
