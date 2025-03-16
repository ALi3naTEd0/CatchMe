import 'dart:io';
import 'dart:convert';
import 'package:path/path.dart' as path;
import 'package:path_provider/path_provider.dart';

class ServerLauncher {
  static final ServerLauncher _instance = ServerLauncher._internal();
  factory ServerLauncher() => _instance;
  ServerLauncher._internal();

  Process? _serverProcess;
  bool get isRunning => _serverProcess != null;

  Future<void> startServer() async {
    if (isRunning) return;

    try {
      _serverProcess = await Process.start(
        'go',
        ['run', 'server/main.go'],
        workingDirectory: path.dirname(Platform.script.path),
      );

      // Log server output
      _serverProcess!.stdout
          .transform(utf8.decoder)
          .transform(const LineSplitter())
          .listen(print);
      _serverProcess!.stderr
          .transform(utf8.decoder)
          .transform(const LineSplitter())
          .listen(print);
    } catch (e) {
      print('Failed to start server: $e');
    }
  }

  Future<void> stopServer() async {
    if (!isRunning) return;
    _serverProcess?.kill();
    _serverProcess = null;
  }
}
