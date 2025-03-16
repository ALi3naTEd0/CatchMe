import 'dart:io';
import 'dart:convert';  // Agregar este import
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

    final serverPath = await _getServerPath();
    print('Starting server from: $serverPath');

    _serverProcess = await Process.start(
      serverPath,
      [],
      environment: {'PORT': '8080'},
    );

    // Log server output usando utf8
    _serverProcess!.stdout
        .transform(utf8.decoder)
        .transform(const LineSplitter())
        .listen(print);
    _serverProcess!.stderr
        .transform(utf8.decoder)
        .transform(const LineSplitter())
        .listen(print);
  }

  Future<String> _getServerPath() async {
    final appDir = await getApplicationSupportDirectory();
    if (Platform.isWindows) {
      return path.join(appDir.path, 'server', 'catchme.exe');
    } else {
      return path.join(appDir.path, 'server', 'catchme');
    }
  }

  Future<void> stopServer() async {
    if (!isRunning) return;
    _serverProcess?.kill();
    _serverProcess = null;
  }
}
