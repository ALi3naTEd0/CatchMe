import 'dart:async';
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
  
  final _statusController = StreamController<bool>.broadcast();
  Stream<bool> get statusStream => _statusController.stream;

  final _logFile = File('logs/server.log');
  
  Future<void> _log(String message) async {
    final timestamp = DateTime.now().toIso8601String();
    final logMessage = '[$timestamp] $message\n';
    
    await Directory('logs').create(recursive: true);
    await _logFile.writeAsString(logMessage, mode: FileMode.append);
    print(logMessage);
  }

  Future<void> startServer() async {
    if (isRunning) return;

    try {
      await _log('=== Iniciando servidor Go ===');
      
      // Verificar puerto en uso
      try {
        final result = await Process.run('lsof', ['-t', '-i:8080']);
        if (result.stdout.toString().isNotEmpty) {
          final pid = result.stdout.toString().trim();
          await Process.run('kill', ['-9', pid]);
          await Future.delayed(const Duration(seconds: 1));
          await _log('Puerto 8080 liberado');
        }
      } catch (e) {
        await _log('Warning: No se pudo liberar el puerto 8080');
      }

      final serverDir = Directory('server');
      if (!serverDir.existsSync()) {
        throw Exception('Server directory not found: ${serverDir.absolute.path}');
      }

      _serverProcess = await Process.start(
        'go',
        ['run', 'main.go'],
        workingDirectory: serverDir.absolute.path,
      );

      await _log('=== Servidor Go iniciado con PID: ${_serverProcess?.pid} ===');

      // Monitorear salida del servidor y actualizar status
      _serverProcess!.stdout.transform(utf8.decoder).listen((data) async {
        await _log('Server stdout: $data');
        if (data.contains('Starting server on :8080')) {
          _statusController.add(true);  // Servidor iniciado
        }
      });

      _serverProcess!.stderr.transform(utf8.decoder).listen((data) async {
        await _log('Server stderr: $data');
        if (data.contains('Starting server on :8080')) {
          _statusController.add(true);  // Servidor iniciado
        }
      });

      // Monitorear si el proceso termina
      _serverProcess!.exitCode.then((code) {
        _log('Server exited with code $code');
        _statusController.add(false);  // Servidor detenido
        _serverProcess = null;
      });

    } catch (e, stack) {
      await _log('Error iniciando servidor: $e\n$stack');
      _statusController.add(false);
      rethrow;
    }
  }

  Future<void> stopServer() async {
    if (!isRunning) return;
    
    try {
      await _log('=== Deteniendo servidor Go ===');
      
      _serverProcess?.kill(ProcessSignal.sigterm);
      await Future.delayed(const Duration(seconds: 1));
      _serverProcess?.kill(ProcessSignal.sigkill);
      
      await _log('=== Servidor detenido ===');
    } catch (e) {
      await _log('Error deteniendo servidor: $e');
    } finally {
      _serverProcess = null;
      _statusController.add(false);
    }
  }
}
