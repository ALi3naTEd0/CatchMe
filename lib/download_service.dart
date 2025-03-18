import 'dart:async';
import 'dart:io';
import 'dart:math';
import 'dart:convert';
import 'dart:typed_data';  // Para Uint8List
import 'package:flutter/foundation.dart';  // Para compute()
import 'package:flutter/widgets.dart';     // Para WidgetsBinding
import 'package:crypto/crypto.dart';       // Para sha256
import 'package:dio/dio.dart';
import 'package:path_provider/path_provider.dart';
import 'package:web_socket_channel/web_socket_channel.dart';
import 'download_item.dart';
import 'download_progress.dart';
import 'websocket_connector.dart';  // Actualizar importación

class DownloadService {
  static final DownloadService _instance = DownloadService._internal();
  factory DownloadService() => _instance;
  DownloadService._internal();

  final _downloads = <String, DownloadItem>{};
  late StreamController<DownloadItem> _downloadController = StreamController<DownloadItem>.broadcast();
  
  // WebSocket Connector
  final _connector = WebSocketConnector();
  bool get _isConnected => _connector.isConnected;
  
  // Retry mechanism
  final Map<String, int> _retryCount = {};
  final Map<String, Timer> _retryTimers = {};
  static const int _maxRetries = 3;

  // Lista de descargas
  Stream<DownloadItem> get downloadStream => _downloadController.stream;
  List<DownloadItem> get downloads => _downloads.values.toList();

  Future<void> init() async {
    if (_downloadController.isClosed) {
      _downloadController = StreamController<DownloadItem>.broadcast();
    }
    
    // Monitorear cambios de conexión
    _connector.statusStream.listen((status) {
      if (status == ConnectionStatus.connected) {
        _onReconnected();
      }
    });
    
    // Intentar conexión con retry
    bool connected = false;
    for (int i = 0; i < 3 && !connected; i++) {
      try {
        await _connector.connect();
        connected = _connector.isConnected;
        if (connected) break;
      } catch (e) {
        print('Connection attempt $i failed: $e');
        await Future.delayed(Duration(seconds: 1));
      }
    }
    
    if (connected) {
      _setupConnectorListeners();
      print('WebSocket connection established successfully');
    } else {
      print('Failed to connect to WebSocket after retries');
    }
  }

  void _setupConnectorListeners() {
    print('Setting up WebSocket listeners');
    _connector.messageStream.listen((message) {
      try {
        final data = jsonDecode(message);
        switch (data['type']) {
          case 'progress':
            _handleProgressUpdate(data);
            break;
          case 'error':
            _handleErrorMessage(data);
            break;
          case 'log':
            _handleLogMessage(data);
            break;
          case 'pong':
            // Solo debug log
            if (kDebugMode) print('Pong received from server');
            break;
          default:
            if (kDebugMode) print('Unknown message type: ${data['type']}');
        }
      } catch (e) {
        print('Error processing message: $e');
      }
    }, onError: (e) {
      print('WebSocket stream error: $e');
    });
  }
  
  // Manejar reconexión y recuperar descargas en progreso
  void _onReconnected() {
    // Intentar resumir descargas en curso
    for (final item in _downloads.values) {
      if (item.status == DownloadStatus.downloading || item.status == DownloadStatus.paused) {
        item.addLog('📡 Reconnected to server, resuming download...');
        resumeDownload(item.url);
      }
    }
  }

  Future<void> startDownload(String url) async {
    try {
      if (!_isConnected) {
        await _connector.connect();
        if (!_isConnected) {
          throw Exception('Server not connected. Please try again.');
        }
      }

      // Verificar si ya existe
      if (_downloads.containsKey(url)) {
        final existing = _downloads[url]!;
        if (existing.status == DownloadStatus.completed) {
          throw Exception('This file has already been downloaded.');
        } else if (existing.status == DownloadStatus.downloading) {
          throw Exception('This file is already being downloaded.');
        } else {
          // Reanudar descarga existente
          resumeDownload(url);
          return;
        }
      }

      // Crear nueva descarga
      final filename = url.split('/').last;
      final item = DownloadItem(
        url: url,
        filename: filename,
        totalBytes: 0,
        status: DownloadStatus.downloading,
      );
      
      item.addLog('🚀 Starting new download');
      _downloads[url] = item;
      _downloadController.add(item);
      
      // Enviar solicitud al servidor
      _connector.send({
        'type': 'start_download',
        'url': url
      });

    } catch (e) {
      print('Error starting download: $e');
      
      // Crear o actualizar item con error
      DownloadItem item;
      if (_downloads.containsKey(url)) {
        item = _downloads[url]!;
      } else {
        item = DownloadItem(
          url: url,
          filename: url.split('/').last,
          totalBytes: 0,
          status: DownloadStatus.error,
        );
        _downloads[url] = item;
      }
      
      item.status = DownloadStatus.error;
      item.error = e.toString();
      item.addLog('❌ Error: ${e.toString()}');
      _downloadController.add(item);
    }
  }

  void pauseDownload(String url) {
    print('Pausing download: $url');
    
    final item = _downloads[url];
    if (item == null) return;
    
    item.addLog('⏸ Pausing download');
    item.status = DownloadStatus.paused;
    item.currentSpeed = 0;
    
    _connector.send({
      'type': 'pause_download',
      'url': url,
    });
    
    _downloadController.add(item);
  }

  void resumeDownload(String url) {
    print('Resuming download: $url');
    
    final item = _downloads[url];
    if (item == null) return;
    
    item.addLog('▶️ Resuming download');
    item.status = DownloadStatus.downloading;
    
    _connector.send({
      'type': 'resume_download',
      'url': url,
    });
    
    _downloadController.add(item);
  }

  void cancelDownload(String url) {
    print('Canceling download: $url');
    
    final item = _downloads[url];
    if (item == null) return;
    
    _connector.send({
      'type': 'cancel_download',
      'url': url,
    });
    
    item.addLog('❌ Download canceled');
    item.status = DownloadStatus.error;
    _downloads.remove(url);
    _downloadController.add(item);
  }

  Stream<String> get progressStream => _connector.messageStream;

  void dispose() {
    // Cancelar todos los timers pendientes
    for (final timer in _retryTimers.values) {
      timer.cancel();
    }
    _retryTimers.clear();
    
    _downloadController.close();
    _connector.dispose();
  }

  void _handleProgressUpdate(Map<String, dynamic> data) {
    final url = data['url'];
    final item = _downloads[url];
    
    if (item != null) {
      // Actualizar estado del item
      item.downloadedBytes = data['bytesReceived'] ?? 0;
      item.totalBytes = data['totalBytes'] ?? 0;
      
      // Actualizar velocidad con historial
      item.updateSpeed((data['speed'] ?? 0.0).toDouble());
      
      // Calcular progreso
      if (item.totalBytes > 0) {
        // Guardar el progreso anterior para comparar
        final oldProgress = item.progress;
        
        // Actualizar progreso
        item.progress = item.downloadedBytes / item.totalBytes;
        
        // Convertir a cadena para asegurar que capturamos cambios decimales
        final newProgressStr = item.formattedProgress;
        final oldProgressStr = (oldProgress * 100).toStringAsFixed(1) + '%';
        
        // Mostrar log para cada cambio en el progreso, incluso decimales
        if (newProgressStr != oldProgressStr) {
          // Usar icono simple
          item.addLog('📥 $newProgressStr');
        }
      }

      // Manejar diferentes estados
      final status = data['status'] as String? ?? 'downloading';
      switch(status) {
        case 'starting':
          item.status = DownloadStatus.downloading;
          item.addLog('📄 File size: ${item.formattedSize}');
          break;
          
        case 'downloading':
          item.status = DownloadStatus.downloading;
          // Resetear contadores de retry si hay progreso
          _retryCount.remove(url);
          break;
          
        case 'completed':
          item.status = DownloadStatus.completed;
          item.progress = 1.0;
          item.addLog('✅ Download completed successfully');
          // Calcular checksum en background
          _verifyChecksum(item);
          break;
      }

      // Notificar UI de cambios
      _downloadController.add(item);
    }
  }

  static String _calculateSHA256(Map<String, dynamic> args) {
    final filePath = args['path'] as String;
    final file = File(filePath);
    
    try {
      final startTime = DateTime.now();
      final fileSize = file.lengthSync();
      print('Starting SHA-256 calculation for $filePath ($fileSize bytes)');
      
      // Leer archivo y calcular hash de una vez
      final bytes = file.readAsBytesSync();
      final digest = sha256.convert(bytes);
      
      final duration = DateTime.now().difference(startTime);
      print('SHA-256 calculation completed in ${duration.inSeconds}.${duration.inMilliseconds % 1000}s');
      
      return digest.toString();
    } catch (e) {
      print('Error calculating checksum: $e');
      return 'Error: $e';
    }
  }

  Future<void> _verifyChecksum(DownloadItem item) async {
    try {
      item.addLog('🔍 Starting file integrity verification...');
      _downloadController.add(item);
      
      // Iniciar cálculo
      final start = DateTime.now();
      item.addLog('🧮 Calculating SHA-256 checksum...');
      _downloadController.add(item);
      
      final path = '${Platform.environment['HOME']}/Downloads/${item.filename}';
      final checksum = await compute(_calculateSHA256, {'path': path});
      
      // Registrar resultado
      final duration = DateTime.now().difference(start);
      item.checksum = checksum;
      item.addLog('✅ Checksum verified in ${duration.inSeconds}s');
      item.addLog('🔐 SHA-256: $checksum');
      _downloadController.add(item);
      
    } catch (e) {
      print('Error verifying checksum: $e');
      item.addLog('⚠️ Could not verify checksum: $e');
      _downloadController.add(item);
    }
  }

  void _handleErrorMessage(Map<String, dynamic> data) {
    final url = data['url'] as String;
    final errorMessage = data['message'] as String? ?? 'Unknown error';
    
    final item = _downloads[url];
    if (item == null) return;
    
    // Añadir log detallado por tipo de error
    if (errorMessage.contains('connection')) {
      item.addLog('🌐 Connection error: $errorMessage');
    } else if (errorMessage.contains('timeout')) {
      item.addLog('⌛ Timeout error: $errorMessage');
    } else {
      item.addLog('❌ Error: $errorMessage');
    }
    
    // Retry más agresivo para errores de red
    final retryCount = _retryCount[url] ?? 0;
    if (retryCount < _maxRetries) {
      _retryCount[url] = retryCount + 1;
      final delay = Duration(seconds: retryCount + 1); // Delay más corto
      
      item.addLog('🔄 Auto-retry ${retryCount + 1}/$_maxRetries in ${delay.inSeconds}s...');
      item.status = DownloadStatus.paused;
      
      _retryTimers[url]?.cancel();
      _retryTimers[url] = Timer(delay, () {
        if (_downloads.containsKey(url)) {
          item.addLog('🔄 Resuming download...');
          resumeDownload(url);
        }
      });
    } else {
      // Mantener la descarga pero marcarla como error
      item.status = DownloadStatus.error;
      item.error = errorMessage;
      item.addLog('💔 Download failed after $_maxRetries retries');
      // No remover la descarga para permitir retry manual
      _downloadController.add(item);
    }
  }

  void _handleLogMessage(Map<String, dynamic> data) {
    final url = data['url'] as String;
    final message = data['message'] as String;
    
    final item = _downloads[url];
    if (item == null) return;
    
    // Agregar ícono según contenido del mensaje
    String formattedMessage = message;
    if (message.contains('size')) {
      formattedMessage = '📊 $message';
    } else if (message.contains('Starting')) {
      formattedMessage = '🚀 $message';
    } else {
      formattedMessage = '💬 $message';
    }
    
    item.addLog(formattedMessage);
    _downloadController.add(item);
  }
}
