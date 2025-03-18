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
    if (isRunning) {
      _statusController.add(true);
      return;
    }

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

      // Limpiar caché de go mod antes de iniciar
      await _log('Limpiando caché de módulos Go...');
      try {
        final cleanResult = await Process.run(
          'go', ['clean', '-modcache'], 
          workingDirectory: serverDir.absolute.path
        );
        
        if (cleanResult.exitCode != 0) {
          await _log('Warning: Error al limpiar caché de Go: ${cleanResult.stderr}');
        } else {
          await _log('Caché de Go limpiada exitosamente');
        }
        
        // Verificar go.mod
        final goModTidyResult = await Process.run(
          'go', ['mod', 'tidy'], 
          workingDirectory: serverDir.absolute.path
        );
        
        if (goModTidyResult.exitCode != 0) {
          await _log('Warning: Error en go mod tidy: ${goModTidyResult.stderr}');
        } else {
          await _log('go mod tidy completado exitosamente');
        }
        
      } catch (e) {
        await _log('Warning: Error en preparación Go: $e');
      }

      // Siempre usar go run para asegurar que se ejecuta el servidor con las actualizaciones
      _serverProcess = await Process.start(
        'go',
        ['run', '.'],
        workingDirectory: serverDir.absolute.path,
      );
      await _log('=== Servidor Go iniciado con go run ===');
      await _log('=== Servidor Go iniciado con PID: ${_serverProcess?.pid} ===');

      // Contador para verificar si el servidor inicia correctamente
      bool serverStarted = false;

      // Monitorear salida del servidor y actualizar status
      _serverProcess!.stdout.transform(utf8.decoder).listen((data) async {
        await _log('Server stdout: $data');
        if (data.contains('Starting server on :8080')) {
          serverStarted = true;
          _statusController.add(true);  // Servidor iniciado
        }
      });

      _serverProcess!.stderr.transform(utf8.decoder).listen((data) async {
        await _log('Server stderr: $data');
      });

      // Monitorear si el proceso termina
      _serverProcess!.exitCode.then((code) async {
        await _log('Server exited with code $code');
        _statusController.add(false);  // Servidor detenido
        _serverProcess = null;
        
        if (!serverStarted && code != 0) {
          // Reintentar con un enfoque alternativo si el servidor falló
          await _log('Intentando método alternativo de inicio...');
          try {
            // Usar 'go build' primero
            await _log('Compilando servidor Go...');
            final buildResult = await Process.run(
              'go', ['build', '-o', 'catchme-server', '.'], 
              workingDirectory: serverDir.absolute.path
            );
            
            if (buildResult.exitCode == 0) {
              await _log('Servidor compilado, intentando ejecutar binario...');
              
              // Ejecutar el binario compilado
              _serverProcess = await Process.start(
                './catchme-server',
                [],
                workingDirectory: serverDir.absolute.path,
              );
              
              await _log('=== Servidor binario iniciado con PID: ${_serverProcess?.pid} ===');
              
              // Configurar listeners para el nuevo proceso
              _serverProcess!.stdout.transform(utf8.decoder).listen((data) async {
                await _log('Binary stdout: $data');
                if (data.contains('Starting server on :8080')) {
                  _statusController.add(true);
                }
              });
              
              _serverProcess!.stderr.transform(utf8.decoder).listen((data) async {
                await _log('Binary stderr: $data');
              });
            } else {
              await _log('Error compilando servidor: ${buildResult.stderr}');
            }
          } catch (e) {
            await _log('Error en proceso de recuperación: $e');
          }
        }
      });

      // Esperar un poco para verificar que el servidor inició correctamente
      await Future.delayed(Duration(seconds: 2));
      
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
