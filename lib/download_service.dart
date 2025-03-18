import 'dart:async';
import 'dart:io';
import 'dart:math';
import 'dart:convert';
import 'dart:typed_data';
import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';
import 'package:crypto/crypto.dart';
import 'package:dio/dio.dart';
import 'package:path_provider/path_provider.dart';
import 'package:web_socket_channel/web_socket_channel.dart';
import 'download_item.dart';
import 'download_progress.dart';
import 'websocket_connector.dart';

// Define modelo para información de chunks
class ChunkInfo {
  final int id;
  final int start;
  final int end;
  final String status;
  final int progress;
  final double speed;
  final int completed;

  ChunkInfo({
    required this.id,
    required this.start,
    required this.end,
    required this.status,
    this.progress = 0,
    this.speed = 0.0,
    this.completed = 0,
  });

  factory ChunkInfo.fromJson(Map<String, dynamic> json) {
    return ChunkInfo(
      id: json['id'] as int,
      start: json['start'] as int,
      end: json['end'] as int,
      status: json['status'] as String,
      progress: json['progress'] as int? ?? 0,
      speed: (json['speed'] as num?)?.toDouble() ?? 0.0,
      completed: json['completed'] as int? ?? 0,
    );
  }

  double get progressPercentage => end > start ? progress / (end - start + 1) : 0.0;
}

// Extension para añadir funcionalidad de chunks a DownloadItem
extension DownloadItemExtensions on DownloadItem {
  // Método para actualizar un chunk
  void updateChunk(ChunkInfo chunkInfo) {
    chunks[chunkInfo.id] = chunkInfo;
  }
  
  // Método para calcular progreso basado en chunks
  String getProgressFromChunks() {
    if (chunks.isNotEmpty) {
      double totalChunkSize = 0; // Changed to double
      double completedBytes = 0; // Changed to double
      
      for (final chunk in chunks.values) {
        final chunkSize = (chunk.end - chunk.start + 1).toDouble(); // Convert to double
        totalChunkSize += chunkSize;
        
        if (chunk.status == 'completed') {
          completedBytes += chunkSize;
        } else {
          completedBytes += chunk.progress.toDouble(); // Convert to double
        }
      }
      
      final percentage = totalChunkSize > 0 
          ? completedBytes / totalChunkSize
          : progress;
          
      return '${(percentage * 100).toStringAsFixed(1)}%';
    }
    
    return '${(progress * 100).toStringAsFixed(1)}%';
  }
}

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
  static const int _maxRetries = 8;  // Aumentado de 3 a 8

  // Lista de descargas
  Stream<DownloadItem> get downloadStream => _downloadController.stream;
  List<DownloadItem> get downloads => _downloads.values.toList();

  // Agregar un conjunto para rastrear URLs canceladas recientemente
  final Set<String> _recentlyCancelled = {};
  final _recentCancelDuration = Duration(seconds: 10);

  // Utilidades de formato
  String _formatSpeed(double bytesPerSecond) {
    if (!bytesPerSecond.isFinite || bytesPerSecond <= 0) return '0 B/s';
    return '${_formatBytes(bytesPerSecond)}/s';
  }

  String _formatBytes(double bytes) {
    if (!bytes.isFinite || bytes <= 0) return '0 B';
    const suffixes = ['B', 'KB', 'MB', 'GB', 'TB'];
    var i = 0;
    while (bytes >= 1024 && i < suffixes.length - 1) {
      bytes /= 1024;
      i++;
    }
    return '${bytes.toStringAsFixed(1)} ${suffixes[i]}';
  }

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
          case 'cancel_confirmed':
            _handleCancelConfirmation(data);
            break;
          case 'server_info':
            _handleServerInfo(data);
            break;
          case 'chunk_init':
            _handleChunkInit(data);
            break;
          case 'chunk_progress':
            _handleChunkProgress(data);
            break;
          case 'pong':
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

  Future<void> startDownload(String url, {bool useChunks = true}) async {
    try {
      if (!_isConnected) {
        await _connector.connect();
        if (!_isConnected) {
          throw Exception('Server not connected. Please try again.');
        }
      }

      // Verificar si fue cancelada recientemente
      if (_recentlyCancelled.contains(url)) {
        print('URL was recently cancelled, waiting a moment before restarting...');
        await Future.delayed(Duration(seconds: 1));
        _recentlyCancelled.remove(url);
      }

      // Antes de hacer cualquier otra cosa, limpiar cualquier rastro de descarga anterior
      // para evitar problemas al reiniciar una descarga previamente cancelada
      _downloads.remove(url);

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
        'url': url,
        'use_chunks': useChunks  // Añadir parámetro para usar chunks
      });

    } catch (e) {
      print('Error starting download: $e');
      
      // Crear o actualizar item con error
      DownloadItem item = DownloadItem(
        url: url,
        filename: url.split('/').last,
        totalBytes: 0,
        status: DownloadStatus.error,
      );
      
      item.status = DownloadStatus.error;
      item.error = e.toString();
      item.addLog('❌ Error: ${e.toString()}');
      _downloads[url] = item;
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
    
    // Marcar URL como recientemente cancelada para evitar conflictos
    _recentlyCancelled.add(url);
    
    // Programar su eliminación del conjunto después de un tiempo
    Timer(_recentCancelDuration, () {
      _recentlyCancelled.remove(url);
    });
    
    final item = _downloads[url];
    if (item == null) return;
    
    _connector.send({
      'type': 'cancel_download',
      'url': url,
    });
    
    item.addLog('❌ Download canceled');
    item.status = DownloadStatus.error;
    
    // Notificar a la UI
    _downloadController.add(item);
    
    // Eliminar definitivamente del mapa INMEDIATAMENTE
    _downloads.remove(url);
    
    print('Download $url completely removed from tracking');
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
    
    // Ignorar actualización si la URL fue recientemente cancelada
    if (_recentlyCancelled.contains(url)) {
      print('Ignoring progress update for cancelled download: $url');
      return;
    }
    
    final item = _downloads[url];
    
    if (item != null) {
      try {
        // Actualizar estado del item
        item.downloadedBytes = data['bytesReceived'] ?? 0;
        item.totalBytes = data['totalBytes'] ?? 0;
        
        // Actualizar velocidad con historial
        double speed = 0.0;
        final rawSpeed = data['speed'];
        if (rawSpeed is double) {
          speed = rawSpeed;
        } else if (rawSpeed is int) {
          speed = rawSpeed.toDouble();
        }
        
        item.updateSpeed(speed);
        
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
      } catch (e) {
        print('Error processing progress update: $e');
      }
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
    
    // Ignorar errores si la URL fue recientemente cancelada
    if (_recentlyCancelled.contains(url)) {
      print('Ignoring error message for cancelled download: $url');
      return;
    }
    
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
    
    // Ignorar mensajes si la URL fue recientemente cancelada
    if (_recentlyCancelled.contains(url)) {
      print('Ignoring log message for cancelled download: $url');
      return;
    }
    
    final message = data['message'] as String;
    
    final item = _downloads[url];
    if (item == null) return;
    
    // Agregar ícono según contenido del mensaje
    String formattedMessage = message;
    if (message.contains('size')) {
      formattedMessage = '📊 $message';
    } else if (message.contains('Starting')) {
      formattedMessage = '🚀 $message';
    } else if (message.contains('Resuming')) {
      formattedMessage = '▶️ $message';
    } else {
      formattedMessage = '💬 $message';
    }
    
    item.addLog(formattedMessage);
    print('Log added to download: $formattedMessage');
    
    // Asegurarnos de notificar a los listeners
    _downloadController.add(item);
  }

  // Manejar confirmación de cancelación del servidor  
  void _handleCancelConfirmation(Map<String, dynamic> data) {
    final url = data['url'] as String;
    print('Server confirmed cancellation of $url');
    
    // Asegurarnos que la URL esté marcada como recientemente cancelada
    _recentlyCancelled.add(url);
    
    // Programar su eliminación del conjunto después de un tiempo
    Timer(_recentCancelDuration, () {
      _recentlyCancelled.remove(url);
    });
  }

  // Manejar información del servidor
  void _handleServerInfo(Map<String, dynamic> data) {
    print('Server capabilities:');
    print('- Implementation: ${data['implementation']}');
    print('- Features: ${data['features']}');
    print('- Chunks supported: ${data['chunks_supported']}');
    
    final chunksSupported = data['chunks_supported'] as bool? ?? false;
    if (chunksSupported) {
      print('✅ Server supports chunked downloads');
    } else {
      print('⚠️ Server does not support chunked downloads');
    }
  }

  // Añadir manejadores para mensajes relacionados con chunks
  void _handleChunkInit(Map<String, dynamic> data) {
    final url = data['url'] as String;
    final chunkData = data['chunk'] as Map<String, dynamic>;
    
    // Ignorar si la URL está en la lista de canceladas
    if (_recentlyCancelled.contains(url)) {
      print('Ignoring chunk init for cancelled download: $url');
      return;
    }
    
    final item = _downloads[url];
    if (item == null) return;
    
    final chunk = ChunkInfo.fromJson(chunkData);
    item.updateChunk(chunk);
    
    item.addLog('🧩 Chunk ${chunk.id+1}: ${_formatBytes((chunk.end - chunk.start + 1).toDouble())} bytes');
    _downloadController.add(item);
  }

  void _handleChunkProgress(Map<String, dynamic> data) {
    final url = data['url'] as String;
    final chunkData = data['chunk'] as Map<String, dynamic>;
    
    // Ignorar si la URL está en la lista de canceladas
    if (_recentlyCancelled.contains(url)) {
      print('Ignoring chunk progress for cancelled download: $url');
      return;
    }
    
    final item = _downloads[url];
    if (item == null) return;
    
    final chunk = ChunkInfo.fromJson(chunkData);
    item.updateChunk(chunk);
    
    // Añadir log solo para cambios significativos
    if (chunk.progress > 0 && chunk.progress % (1024*1024) < 1024*10) { // Cada ~1MB
      final chunkProgress = (chunk.progress * 100 / (chunk.end - chunk.start + 1)).toStringAsFixed(0);
      item.addLog('🧩 Chunk ${chunk.id+1}: $chunkProgress% at ${_formatSpeed(chunk.speed)}');
    }
    
    // Actualizar UI
    _downloadController.add(item);
  }
}