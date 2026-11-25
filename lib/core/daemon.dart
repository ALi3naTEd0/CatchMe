/// Starts and stops `catchmed`, the Go download daemon bundled with the app.
library;

import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math';

import 'package:logging/logging.dart';
import 'package:path/path.dart' as p;

class DaemonException implements Exception {
  DaemonException(this.message);
  final String message;
  @override
  String toString() => message;
}

/// Runs the daemon as a child process on a free loopback port, with a fresh
/// random token. The daemon is started with -exit-with-parent, so it stops by
/// itself when the app closes or crashes: it never lingers in the background.
class Daemon {
  Daemon({this.downloadDir, this.stateDir}) : token = _randomToken();

  final String token;

  /// Override where files and the download list go; null uses the daemon's
  /// defaults (~/Downloads and the user config folder).
  final String? downloadDir;
  final String? stateDir;
  String? address; // host:port once running
  Process? _process;
  final _log = Logger('Daemon');

  bool get isRunning => _process != null;

  Future<String> start() async {
    if (address != null && _process != null) return address!;
    final exe = findBinary();
    _log.info('starting $exe');
    final proc = await Process.start(
      exe,
      [
        '-addr',
        '127.0.0.1:0',
        '-exit-with-parent',
        if (downloadDir != null) ...['-dir', downloadDir!],
        if (stateDir != null) ...['-state', stateDir!],
      ],
      environment: {'CATCHME_TOKEN': token},
    );
    _process = proc;

    proc.stderr.transform(utf8.decoder).transform(const LineSplitter()).listen((l) => _log.fine(l));

    // The first stdout line says where it listens. Keep reading stdout after
    // that so the pipe stays open.
    final ready = Completer<String>();
    proc.stdout.transform(utf8.decoder).transform(const LineSplitter()).listen((line) {
      if (ready.isCompleted) return;
      try {
        final j = jsonDecode(line) as Map<String, dynamic>;
        if (j['ready'] == true && j['addr'] is String) ready.complete(j['addr'] as String);
      } catch (_) {
        /* not the ready line */
      }
    });
    unawaited(
      proc.exitCode.then((code) {
        _log.warning('daemon exited with code $code');
        _process = null;
        address = null;
        if (!ready.isCompleted) {
          ready.completeError(DaemonException('The download engine stopped while starting (code $code).'));
        }
      }),
    );

    address = await ready.future.timeout(
      const Duration(seconds: 10),
      onTimeout: () {
        proc.kill();
        throw DaemonException('The download engine did not start within 10 seconds.');
      },
    );
    return address!;
  }

  /// Asks the daemon to stop by closing its stdin; it pauses running
  /// downloads, saves their progress and exits.
  Future<void> stop() async {
    final proc = _process;
    if (proc == null) return;
    await proc.stdin.close();
    await proc.exitCode.timeout(
      const Duration(seconds: 10),
      onTimeout: () {
        proc.kill();
        return -1;
      },
    );
  }

  /// Looks for the daemon next to the app first (installed builds), then in
  /// the source tree (development: `make build` in catchmed/).
  static String findBinary() {
    final name = Platform.isWindows ? 'catchmed.exe' : 'catchmed';
    final exeDir = p.dirname(Platform.resolvedExecutable);
    final candidates = [
      p.join(exeDir, name), // Windows, and Linux bundles
      p.join(exeDir, 'lib', name), // Linux bundle lib/ folder
      p.join(exeDir, '..', 'Resources', name), // macOS .app
      p.join(Directory.current.path, 'catchmed', 'bin', name), // development
    ];
    for (final c in candidates) {
      if (File(c).existsSync()) return p.normalize(c);
    }
    throw DaemonException(
      'Could not find the download engine ($name).\n'
      'Developers: run "make build" inside catchmed/ first.',
    );
  }

  static String _randomToken() {
    final r = Random.secure();
    return List.generate(32, (_) => r.nextInt(256).toRadixString(16).padLeft(2, '0')).join();
  }
}
