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
    
    // Asegurar que el directorio logs existe
    await Directory('logs').create(recursive: true);
    
    // Escribir al archivo
    await _logFile.writeAsString(
      logMessage,
      mode: FileMode.append,
    );
    
    // También imprimir a consola
    print(logMessage);
  }

  Future<void> startServer() async {
    if (isRunning) return;

    try {
      await _log('=== Iniciando servidor Go ===');
      
      // Limpiar puerto 8080
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
      
      final serverDir = Directory('server');  // Cambio aquí: buscar en la raíz
      if (!serverDir.existsSync()) {
        throw Exception('Directorio server/ no encontrado en: ${serverDir.absolute.path}');
      }

      await _log('Iniciando servidor desde: ${serverDir.absolute.path}');
      
      _serverProcess = await Process.start(
        'go',
        ['run', 'main.go'],
        workingDirectory: serverDir.absolute.path,
      );

      await _log('=== Servidor Go iniciado con PID: ${_serverProcess?.pid} ===');
      
      // Esperar a que el servidor esté listo
      final completer = Completer<bool>();
      
      // Manejar stdout y stderr juntos ya que Go puede usar cualquiera para logs
      Future.wait([
        _serverProcess!.stdout.transform(utf8.decoder).forEach((data) async {
          await _log('Server stdout: $data');
          if (data.contains('Starting server on :8080')) {
            if (!completer.isCompleted) {
              completer.complete(true);
              _statusController.add(true);
            }
          }
        }),
        _serverProcess!.stderr.transform(utf8.decoder).forEach((data) async {
          await _log('Server stderr: $data');
          if (data.contains('Starting server on :8080')) {
            if (!completer.isCompleted) {
              completer.complete(true);
              _statusController.add(true);
            }
          }
        })
      ]);

      // Esperar la inicialización con timeout
      await completer.future.timeout(
        const Duration(seconds: 5),
        onTimeout: () => throw Exception('Servidor no respondió en 5 segundos'),
      );

    } catch (e, stack) {
      await _log('=== Error iniciando servidor: $e ===');
      await _log('Stack trace: $stack');
      _statusController.add(false);
      rethrow;  // Para debug
    }
  }

  Future<String> _getServerPath() async {
    return 'go'; // Simplemente retornamos 'go' ya que estamos usando 'go run'
  }

  Future<void> stopServer() async {
    if (!isRunning) return;
    
    try {
      await _log('=== Deteniendo servidor Go ===');
      
      // Intentar detener el proceso gracefully primero
      _serverProcess?.kill(ProcessSignal.sigterm);
      
      // Esperar un momento y forzar el cierre si sigue vivo
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
