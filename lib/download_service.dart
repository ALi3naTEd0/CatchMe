import 'dart:async';
import 'dart:convert';
import 'package:flutter/foundation.dart';
import 'package:shared_preferences/shared_preferences.dart'; // Añadir este import
import 'package:logging/logging.dart';
import 'download_item.dart';
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

  double get progressPercentage =>
      end > start ? progress / (end - start + 1) : 0.0;
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
      double totalChunkSize = 0;
      double completedBytes = 0;

      for (final chunk in chunks.values) {
        final chunkSize = (chunk.end - chunk.start + 1).toDouble();
        totalChunkSize += chunkSize;

        if (chunk.status == 'completed') {
          completedBytes += chunkSize;
        } else {
          completedBytes += chunk.progress.toDouble();
        }
      }

      final percentage =
          totalChunkSize > 0 ? completedBytes / totalChunkSize : progress;

      return '${(percentage * 100).toStringAsFixed(1)}%';
    }

    return '${(progress * 100).toStringAsFixed(1)}%';
  }
}

class DownloadService {
  static final DownloadService _instance = DownloadService._internal();
  factory DownloadService() => _instance;

  // Add logger instance
  final _logger = Logger('DownloadService');

  DownloadService._internal() {
    // Initialize logging
    Logger.root.level = Level.ALL;
    Logger.root.onRecord.listen((record) {
      if (kDebugMode) {
        debugPrint('${record.level.name}: ${record.time}: ${record.message}');
      }
    });
  }

  final _downloads = <String, DownloadItem>{};
  late StreamController<DownloadItem> _downloadController =
      StreamController<DownloadItem>.broadcast();

  // WebSocket Connector
  final _connector = WebSocketConnector();
  bool get _isConnected => _connector.isConnected;

  // Retry mechanism
  final Map<String, int> _retryCount = {};
  final Map<String, Timer> _retryTimers = {};
  static const int _maxRetries = 8; // Aumentado de 3 a 8

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
        _logger.warning('Connection attempt $i failed: $e');
        await Future.delayed(Duration(seconds: 1));
      }
    }

    if (connected) {
      _setupConnectorListeners();
      _logger.info('WebSocket connection established successfully');
    } else {
      _logger.severe('Failed to connect to WebSocket after retries');
    }
  }

  void _setupConnectorListeners() {
    _logger.info('Setting up WebSocket listeners');
    _connector.messageStream.listen(
      (message) {
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
              _logger.fine('Pong received from server');
              break;
            case 'pause_confirmed':
              _handlePauseConfirmation(data);
              break;
            case 'resume_confirmed':
              _handleResumeConfirmation(data);
              break;
            case 'checksum_result':
              _handleChecksumResult(data);
              break;
            case 'download_complete':
              _handleDownloadComplete(data);
              break;
            case 'merge_start':
              _handleMergeStart(data);
              break;
            default:
              _logger.warning('Unknown message type: ${data['type']}');
          }
        } catch (e) {
          _logger.severe('Error processing message: $e');
        }
      },
      onError: (e) {
        _logger.severe('WebSocket stream error: $e');
      },
    );
  }

  // Manejar reconexión y recuperar descargas en progreso
  void _onReconnected() {
    // Intentar resumir descargas en curso
    for (final item in _downloads.values) {
      if (item.status == DownloadStatus.downloading ||
          item.status == DownloadStatus.paused) {
        item.addLog('📡 Reconnected to server, resuming download...');
        resumeDownload(item.url);
      }
    }
  }

  Future<void> startDownload(String url, {bool? useChunks}) async {
    try {
      if (!_isConnected) {
        await _connector.connect();
        if (!_isConnected) {
          throw Exception('Server not connected. Please try again.');
        }
      }

      // Verificar si fue cancelada recientemente
      if (_recentlyCancelled.contains(url)) {
        _logger.info(
          'URL was recently cancelled, waiting a moment before restarting...',
        );
        await Future.delayed(Duration(seconds: 1));
        _recentlyCancelled.remove(url);
      }

      // Antes de hacer cualquier otra cosa, limpiar cualquier rastro de descarga anterior
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

      // Obtener preferencia actual para chunks si no se especificó
      if (useChunks == null) {
        final prefs = await SharedPreferences.getInstance();
        useChunks = prefs.getBool('use_chunked_downloads') ?? true;
      }

      // Enviar solicitud al servidor
      _connector.send({
        'type': 'start_download',
        'url': url,
        'use_chunks': useChunks,
      });
    } catch (e) {
      _logger.severe('Error starting download: $e');

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

  // Mejorar las funciones de pausa/resume para asegurar que actualizan el estado correctamente
  void pauseDownload(String url) {
    _logger.info('Client: Sending pause command for: $url');

    final item = _downloads[url];
    if (item == null) return;

    // Set temporary state immediately
    if (item.status == DownloadStatus.downloading) {
      item.tempStatus = 'pausing';
      item.addLog('⏸ Pausing download...');

      // Store last known speed before pausing
      _updateSpeedHistory(url, item.currentSpeed);

      _downloadController.add(item);
      _sendCommandWithRetry('pause_download', url, 3);
    }
  }

  void resumeDownload(String url) {
    _logger.info('Client: Sending resume command for: $url');

    final item = _downloads[url];
    if (item == null) return;

    // Set temporary state immediately
    if (item.status == DownloadStatus.paused) {
      item.tempStatus = 'resuming';
      item.addLog('▶️ Resuming download...');

      // Use stored speed history for optimizing chunks
      final avgSpeed = _getAverageSpeed(url);
      if (avgSpeed > 0) {
        _connector.send({
          'type': 'resume_download',
          'url': url,
          'previous_speed': avgSpeed,
        });
      } else {
        _sendCommandWithRetry('resume_download', url, 3);
      }

      _downloadController.add(item);
    }
  }

  // Improve speed history tracking
  double _getAverageSpeed(String url) {
    final history = _speedHistory[url];
    if (history == null || history.isEmpty) return 0;

    return history.reduce((a, b) => a + b) / history.length;
  }

  void cancelDownload(String url) {
    _logger.info('Canceling download: $url');

    // Marcar URL como recientemente cancelada para evitar conflictos
    _recentlyCancelled.add(url);
    // Programar su eliminación del conjunto después de un tiempo
    Timer(_recentCancelDuration, () {
      _recentlyCancelled.remove(url);
    });

    final item = _downloads[url];
    if (item == null) return;

    _connector.send({'type': 'cancel_download', 'url': url});

    item.addLog('❌ Download canceled');
    item.status = DownloadStatus.error;
    // Notificar a la UI
    _downloadController.add(item);

    // Eliminar definitivamente del mapa INMEDIATAMENTE
    _downloads.remove(url);
    _logger.info('Download $url completely removed from tracking');
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
    if (_recentlyCancelled.contains(url)) return;

    final item = _downloads[url];
    if (item != null) {
      try {
        final newBytes = data['bytesReceived'] ?? 0;
        final totalBytes = data['totalBytes'] ?? 0;
        final status = data['status']?.toString() ?? "downloading";

        // Update progress and bytes
        item.downloadedBytes = newBytes;
        item.totalBytes = totalBytes;

        // Calculate precise progress
        if (totalBytes > 0) {
          // Special case handling to ensure smooth transition to 100%
          if (status == "completed") {
            item.progress = 1.0;
          } else if (newBytes >= totalBytes - 1) {
            // If we're at the last byte but not marked completed, cap at 99.9%
            item.progress = 0.999;
          } else {
            item.progress = (newBytes / totalBytes).clamp(0.0, 0.999);
          }
        }

        _downloadController.add(item);
      } catch (e) {
        _logger.severe('Error processing progress update: $e');
      }
    }
  }

  void _handleChecksumResult(Map<String, dynamic> data) {
    final url = data['url'];
    final checksum = data['checksum'] as String;
    final duration = data['duration'] as int; // en milisegundos

    final item = _downloads[url];
    if (item == null) return;

    item.checksum = checksum;
    final seconds = duration / 1000.0;

    // Only add checksum log if we don't already have it
    if (!item.logs.any((log) => log.contains(checksum))) {
      item.addLog(
        '🔑 SHA-256 checksum: $checksum (calculated in ${seconds.toStringAsFixed(1)}s)',
      );
    }

    item.status = DownloadStatus.completed;
    item.progress = 1.0;
    _downloadController.add(item);
    _logger.info('Checksum completed for $url: $checksum');
  }

  void _handleErrorMessage(Map<String, dynamic> data) {
    final url = data['url'] as String;
    // Ignore errors if URL was recently cancelled
    if (_recentlyCancelled.contains(url)) {
      _logger.info('Ignoring error message for cancelled download: $url');
      return;
    }

    final errorMessage = data['message'] as String? ?? 'Unknown error';
    final item = _downloads[url];
    if (item == null) return;

    // Add detailed error log
    if (errorMessage.contains('connection')) {
      item.addLog('🌐 Connection error: $errorMessage');
    } else if (errorMessage.contains('timeout')) {
      item.addLog('⌛ Timeout error: $errorMessage');
    } else {
      item.addLog('❌ Error: $errorMessage');
    }

    // Only retry if not paused
    if (item.status != DownloadStatus.paused) {
      final retryCount = _retryCount[url] ?? 0;
      if (retryCount < _maxRetries) {
        _retryCount[url] = retryCount + 1;
        final delay = Duration(seconds: retryCount + 1);
        item.addLog(
          '🔄 Auto-retry ${retryCount + 1}/$_maxRetries in ${delay.inSeconds}s...',
        );
        _retryTimers[url]?.cancel();
        _retryTimers[url] = Timer(delay, () {
          if (_downloads.containsKey(url) &&
              item.status != DownloadStatus.paused) {
            resumeDownload(url);
          }
        });
      } else {
        // Mark as error if max retries reached
        item.status = DownloadStatus.error;
        item.error = errorMessage;
        item.addLog('💔 Download failed after $_maxRetries retries');
        _downloadController.add(item);
      }
    }
  }

  void _handleLogMessage(Map<String, dynamic> data) {
    final url = data['url'];
    if (_recentlyCancelled.contains(url)) return;

    final message = data['message'] as String;
    // Remove unused variable 'force'
    final item = _downloads[url];
    if (item == null) return;

    // Special case for important progress updates to ensure proper sequence
    if (message == "📥 99.9%") {
      _logger.info('Received 99.9% progress update for $url');
      item.addLog(message);
      item.progress = 0.999; // Ensure progress bar shows proper value
      _downloadController.add(item);
      return;
    }

    if (message == "📥 100.0%") {
      _logger.info('Received 100.0% progress update for $url');
      item.addLog(message);
      item.progress = 1.0; // Force to exactly 1.0
      _downloadController.add(item);
      return;
    }

    if (message.startsWith('🔄 Merging chunks')) {
      _logger.info('Merging chunks for $url');
      item.addLog(message);
      _downloadController.add(item);
      return;
    }

    if (message == "✅ Download completed successfully") {
      _logger.info('Download completed for $url');
      item.addLog(message);
      item.status = DownloadStatus.completed;
      _downloadController.add(item);
      return;
    }

    // Handle all other messages normally
    item.addLog(message);
    _downloadController.add(item);
  }

  // Manejar confirmación de cancelación del servidor
  void _handleCancelConfirmation(Map<String, dynamic> data) {
    final url = data['url'] as String;
    _logger.info('Server confirmed cancellation of $url');
    // Asegurarnos que la URL esté marcada como recientemente cancelada
    _recentlyCancelled.add(url);
    // Programar su eliminación del conjunto después de un tiempo
    Timer(_recentCancelDuration, () {
      _recentlyCancelled.remove(url);
    });
  }

  // Manejar información del servidor
  void _handleServerInfo(Map<String, dynamic> data) {
    _logger.info('Server capabilities:');
    _logger.info('- Implementation: ${data['implementation']}');
    _logger.info('- Features: ${data['features']}');
    _logger.info('- Chunks supported: ${data['chunks_supported']}');
    final chunksSupported = data['chunks_supported'] as bool? ?? false;
    if (chunksSupported) {
      _logger.info('✅ Server supports chunked downloads');
    } else {
      _logger.warning('⚠️ Server does not support chunked downloads');
    }
  }

  // Añadir manejadores para mensajes relacionados con chunks
  void _handleChunkInit(Map<String, dynamic> data) {
    final url = data['url'] as String;
    final chunkData = data['chunk'] as Map<String, dynamic>;
    if (_recentlyCancelled.contains(url)) {
      _logger.info('Ignoring chunk init for cancelled download: $url');
      return;
    }

    final item = _downloads[url];
    if (item == null) return;

    final chunk = ChunkInfo.fromJson(chunkData);
    item.updateChunk(chunk);

    _downloadController.add(item);
  }

  void _handleChunkProgress(Map<String, dynamic> data) {
    final url = data['url'] as String;
    final chunkData = data['chunk'] as Map<String, dynamic>;
    if (_recentlyCancelled.contains(url)) return;

    final item = _downloads[url];
    if (item == null) return;

    final chunk = ChunkInfo.fromJson(chunkData);
    item.updateChunk(chunk);

    final now = DateTime.now();
    // More frequent updates (100ms instead of 200ms)
    final shouldUpdateUI =
        item.lastProgressLog == null ||
        now.difference(item.lastProgressLog!) > Duration(milliseconds: 100);

    if (shouldUpdateUI || chunk.status == 'completed') {
      item.lastProgressLog = now;

      // Calculate overall speed from chunks
      double totalSpeed = 0;
      for (final chunk in item.chunks.values) {
        if (chunk.status == 'active') {
          totalSpeed += chunk.speed;
        }
      }

      // Update speed from chunks
      if (totalSpeed > 0) {
        item.updateSpeed(totalSpeed);
        _updateSpeedHistory(url, totalSpeed);
      }

      _updateOverallProgress(item);
    }

    // Verificar si debemos incluir logs detallados de chunks
    bool includeChunkDetails = false;
    SharedPreferences.getInstance().then((prefs) {
      includeChunkDetails = prefs.getBool('include_chunk_details') ?? false;
    });

    // Solo mostrar logs de chunks si está habilitado explícitamente
    if (includeChunkDetails &&
        chunk.status == 'active' &&
        chunk.progress > 0 &&
        // Aumentar el intervalo para reducir spam
        (chunk.progress % (100 * 1024 * 1024) < 1024 * 1024)) {
      // Solo cada ~100MB
      final chunkProgress = (chunk.progressPercentage * 100).toStringAsFixed(0);
      item.addLog(
        '🧩 Chunk ${chunk.id + 1}: $chunkProgress% at ${_formatSpeed(chunk.speed)}',
      );
    }
  }

  // Método para calcular y actualizar el progreso general basado en chunks - corregido para evitar valores > 100%
  void _updateOverallProgress(DownloadItem item) {
    if (item.chunks.isEmpty) return;

    double totalDownloaded = 0;
    double totalSize = 0;

    // Calculate total progress from chunks
    for (final chunk in item.chunks.values) {
      double chunkSize = (chunk.end - chunk.start + 1).toDouble();
      totalSize += chunkSize;

      if (chunk.status == 'completed') {
        totalDownloaded += chunkSize;
      } else if (chunk.status == 'active' && chunk.progress > 0) {
        if (chunk.progress >= chunkSize - 32) {
          // Align with server threshold
          totalDownloaded += chunkSize;
        } else {
          totalDownloaded += chunk.progress.toDouble();
        }
      }
    }

    if (totalSize > 0) {
      totalDownloaded = totalDownloaded.clamp(0, totalSize);
      item.totalBytes = totalSize.toInt();
      item.downloadedBytes = totalDownloaded.toInt();
      item.progress = (totalDownloaded / totalSize).clamp(0, 1);

      // More frequent progress messages
      final progressPercent = (item.progress * 100).toStringAsFixed(1);
      final lastLog = item.logs.isNotEmpty ? item.logs.last : "";

      if (!lastLog.contains(progressPercent) &&
          (item.progress >= 0.999 || // Always show 100%
              (item.progress - item.lastLoggedProgress).abs() >= 0.001)) {
        // More frequent updates (0.1%)
        item.addLog('📥 $progressPercent%');
        item.lastLoggedProgress = item.progress;
      }
    }

    _downloadController.add(item);
  }

  // Nuevo método para enviar comandos con retry
  void _sendCommandWithRetry(String commandType, String url, int maxRetries) {
    int retries = 0;
    void attemptSend() {
      try {
        _connector.send({'type': commandType, 'url': url});
        _logger.info('Command $commandType sent successfully for $url');
      } catch (e) {
        _logger.warning('Error sending $commandType command: $e');
        retries++;
        if (retries < maxRetries) {
          _logger.info(
            'Retrying $commandType command ($retries/$maxRetries)...',
          );
          Future.delayed(Duration(milliseconds: 200), attemptSend);
        } else {
          _logger.severe(
            'Failed to send $commandType after $maxRetries attempts.',
          );
          // Revertir estado temporal si hay falla definitiva
          final item = _downloads[url];
          if (item != null) {
            item.tempStatus = null;
            _downloadController.add(item);
          }
        }
      }
    }

    attemptSend();
  }

  // Agregar nuevos handlers para confirmaciones
  void _handlePauseConfirmation(Map<String, dynamic> data) {
    final url = data['url'] as String;
    _logger.info('Server confirmed pause of $url');

    final item = _downloads[url];
    if (item != null) {
      // Limpiar tempStatus y actualizar estado definitivo
      item.tempStatus = null;
      item.pauseRetries = 0;
      item.status = DownloadStatus.paused;
      item.currentSpeed = 0;
      item.addLog('⏸ Download paused successfully');

      _logger.info('Download state updated to paused for: $url');
      _downloadController.add(item);
    } else {
      _logger.warning(
        'Cannot update paused state: download not found for $url',
      );
    }
  }

  void _handleResumeConfirmation(Map<String, dynamic> data) {
    final url = data['url'] as String;
    _logger.info('Server confirmed resume of $url');

    final item = _downloads[url];
    if (item != null) {
      // Limpiar tempStatus y actualizar estado definitivo
      item.tempStatus = null;
      item.resumeRetries = 0;
      item.status = DownloadStatus.downloading;
      item.addLog('▶️ Download resumed successfully');

      _logger.info('Download state updated to downloading for: $url');
      _downloadController.add(item);
    } else {
      _logger.warning(
        'Cannot update resumed state: download not found for $url',
      );
    }
  }

  // Add speed history
  final Map<String, List<double>> _speedHistory = {};

  void _updateSpeedHistory(String url, double speed) {
    if (!_speedHistory.containsKey(url)) {
      _speedHistory[url] = [];
    }

    final history = _speedHistory[url]!;
    history.add(speed);

    // Keep last 10 speed samples
    if (history.length > 10) {
      history.removeAt(0);
    }

    // Analyze speed trend and adjust chunk count
    _optimizeChunks(url);
  }

  void _optimizeChunks(String url) {
    final history = _speedHistory[url];
    if (history == null || history.length < 5) return;

    // Calculate average speed
    final avgSpeed = history.reduce((a, b) => a + b) / history.length;

    // Send optimization command to server
    _connector.send({'type': 'optimize_chunks', 'url': url, 'speed': avgSpeed});
  }

  void _handleDownloadComplete(Map<String, dynamic> data) {
    final url = data['url'];
    if (_recentlyCancelled.contains(url)) return;

    final item = _downloads[url];
    if (item != null) {
      // Force completion state
      item.progress = 1.0;
      item.downloadedBytes = item.totalBytes;

      // Always ensure proper message sequence
      if (!item.logs.any((log) => log.contains('100.0%'))) {
        item.addLog('📥 100.0%');
      }
      if (!item.logs.any((log) => log.contains('Merging chunks'))) {
        item.addLog('🔄 Merging chunks...');
      }
      _downloadController.add(item);
    }
  }

  void _handleMergeStart(Map<String, dynamic> data) {
    final url = data['url'];
    if (_recentlyCancelled.contains(url)) return;

    final item = _downloads[url];
    if (item != null &&
        !item.logs.any((log) => log.contains("Merging chunks"))) {
      item.addLog('🔄 Merging chunks...');
      _downloadController.add(item);
    }
  }
}
