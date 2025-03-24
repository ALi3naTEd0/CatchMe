import 'dart:async';
import 'dart:io';
import 'dart:convert';
import 'package:logging/logging.dart';

class ServerLauncher {
  static final ServerLauncher _instance = ServerLauncher._internal();
  factory ServerLauncher() => _instance;

  final _logger = Logger('ServerLauncher');

  ServerLauncher._internal();

  Process? _serverProcess;
  bool get isRunning => _serverProcess != null;

  final _statusController = StreamController<bool>.broadcast();
  Stream<bool> get statusStream => _statusController.stream;

  final _logFile = File('logs/server.log');

  bool _serviceMode = false;

  Future<void> _log(String message) async {
    final timestamp = DateTime.now().toIso8601String();
    final logMessage = '[$timestamp] $message\n';

    await Directory('logs').create(recursive: true);
    await _logFile.writeAsString(logMessage, mode: FileMode.append);
    _logger.info(message);
  }

  Future<void> startServer({bool asService = false}) async {
    if (isRunning) {
      _statusController.add(true);
      return;
    }

    try {
      await _log('=== Iniciando servidor Go ===');
      _serviceMode = asService;

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
        throw Exception(
          'Server directory not found: ${serverDir.absolute.path}',
        );
      }

      // Limpiar caché antes de iniciar
      await _log('Preparando servidor...');

      // Si es modo servicio, añadir el argumento correspondiente
      final List<String> args = ['run', '.'];
      if (_serviceMode) {
        args.add('--service');
        await _log('Iniciando en modo servicio...');
      }

      _serverProcess = await Process.start(
        'go',
        args,
        workingDirectory: serverDir.absolute.path,
      );

      if (_serviceMode) {
        await _log('=== Servidor Go iniciado como servicio ===');
      } else {
        await _log('=== Servidor Go iniciado en modo normal ===');
      }
      await _log('=== PID: ${_serverProcess?.pid} ===');

      // Monitorear salida y error
      _monitorServerOutput();

      // Esperar para verificar que inició correctamente
      await Future.delayed(Duration(seconds: 2));
    } catch (e, stack) {
      await _log('Error iniciando servidor: $e\n$stack');
      _statusController.add(false);
      rethrow;
    }
  }

  void _monitorServerOutput() {
    _serverProcess!.stdout.transform(utf8.decoder).listen((data) async {
      await _log('Server stdout: $data');
      if (data.contains('Starting server on :8080') ||
          data.contains('CatchMe service started')) {
        _statusController.add(true); // Servidor iniciado
      }
    });

    _serverProcess!.stderr.transform(utf8.decoder).listen((data) async {
      await _log('Server stderr: $data');
    });

    // Monitorear si el proceso termina
    _serverProcess!.exitCode.then((code) async {
      await _log('Server exited with code $code');
      _statusController.add(false); // Servidor detenido
      _serverProcess = null;

      // No reintentar si estamos en modo servicio y fue terminación normal
      if (!_serviceMode || code != 0) {
        _attemptRecovery();
      }
    });
  }

  Future<void> _attemptRecovery() async {
    // ... implementar lógica de recuperación aquí ...
  }

  Future<void> stopServer() async {
    if (!isRunning) return;

    try {
      await _log('=== Deteniendo servidor Go ===');

      if (_serviceMode) {
        // En modo servicio, enviar señal SIGTERM para un apagado limpio
        _serverProcess?.kill(ProcessSignal.sigterm);
        await Future.delayed(
          const Duration(seconds: 3),
        ); // Dar más tiempo para limpieza en modo servicio
      }

      // Si todavía está ejecutando, forzar terminación
      if (_serverProcess != null) {
        _serverProcess?.kill(ProcessSignal.sigkill);
      }

      await _log('=== Servidor detenido ===');
    } catch (e) {
      await _log('Error deteniendo servidor: $e');
    } finally {
      _serverProcess = null;
      _statusController.add(false);
    }
  }
}
