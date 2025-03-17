import 'dart:async';
import 'dart:io';
import 'dart:math';
import 'dart:convert';
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

  // Lista de descargas
  Stream<DownloadItem> get downloadStream => _downloadController.stream;
  List<DownloadItem> get downloads => _downloads.values.toList();

  void init() {
    if (_downloadController.isClosed) {
      _downloadController = StreamController<DownloadItem>.broadcast();
    }
    
    _connector.connect();
    _setupConnectorListeners();
  }

  void _setupConnectorListeners() {
    print('Setting up WebSocket listeners');
    _connector.messageStream.listen((message) {
      try {
        final data = jsonDecode(message);
        if (data['type'] == 'progress') {
          // Procesar progreso inmediatamente
          _handleProgressUpdate(data);
        } else {
          // Otros mensajes
          _handleServerMessage(message);
        }
      } catch (e) {
        print('Error processing message: $e');
      }
    });
  }

  Future<void> startDownload(String url) async {
    try {
      print('\n=== Starting new download ===');
      print('URL: $url');
      
      if (!_isConnected) {
        throw Exception('Server not connected');
      }

      final item = DownloadItem(
        url: url,
        filename: url.split('/').last,
        totalBytes: 0,
        status: DownloadStatus.downloading,
      );
      
      _downloads[url] = item;
      _downloadController.add(item);
      
      _connector.send({
        'type': 'start_download',
        'url': url
      });

    } catch (e) {
      print('Error starting download: $e');
      final item = _downloads[url];
      if (item != null) {
        item.status = DownloadStatus.error;
        item.error = e.toString();
        _downloadController.add(item);
      }
    }
  }

  void pauseDownload(String url) {
    print('Pausing download: $url');
    
    _connector.send({
      'type': 'pause_download',
      'url': url,
    });
    
    final item = _downloads[url];
    if (item != null) {
      item.status = DownloadStatus.paused;
      item.currentSpeed = 0;
      _downloadController.add(item);
    }
  }

  void resumeDownload(String url) {
    print('Resuming download: $url');
    
    _connector.send({
      'type': 'resume_download',
      'url': url,
    });
    
    final item = _downloads[url];
    if (item != null) {
      item.status = DownloadStatus.downloading;
      _downloadController.add(item);
    }
  }

  void cancelDownload(String url) {
    print('Canceling download: $url');
    
    _connector.send({
      'type': 'cancel_download',
      'url': url,
    });
    
    final item = _downloads[url];
    if (item != null) {
      _downloads.remove(url);
      _downloadController.add(item..status = DownloadStatus.error);
    }
  }

  String _getFilename(Response response) {
    final disposition = response.headers.value('content-disposition');
    if (disposition != null && disposition.contains('filename=')) {
      return disposition.split('filename=')[1].replaceAll('"', '');
    }
    final uri = Uri.parse(response.realUri.toString());
    return uri.pathSegments.last;
  }

  // Cambiar el tipo de retorno para que coincida
  Stream<String> get progressStream => _connector.messageStream;

  @override
  void dispose() {
    _downloadController.close();
    _connector.dispose();
  }

  void _handleServerMessage(String message) {
    try {
      final data = jsonDecode(message);
      print('Decoded message: $data');
      
      switch(data['type']) {
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
          print('Received pong');
          break;
        default:
          print('Unknown message type: ${data['type']}');
      }
    } catch (e) {
      print('Error processing message: $e');
    }
  }

  void _handleProgressUpdate(Map<String, dynamic> data) {
    final url = data['url'];
    final item = _downloads[url];
    
    if (item != null) {
      // Actualizar estado
      item.downloadedBytes = data['bytesReceived'] ?? 0;
      item.totalBytes = data['totalBytes'] ?? 0;
      item.currentSpeed = (data['speed'] ?? 0.0).toDouble();
      
      // Calcular progreso y velocidad
      if (item.totalBytes > 0) {
        item.progress = item.downloadedBytes / item.totalBytes;
        // Log UI
        item.addLog('${(item.progress * 100).toStringAsFixed(1)}% - ${item.formattedSpeed}');
      }

      // Estado
      switch(data['status']) {
        case 'completed':
          item.status = DownloadStatus.completed;
          // Más logs...
          break;
        case 'downloading':
          item.status = DownloadStatus.downloading;
          break;
        // ...otros casos...
      }

      // Notificar UI INMEDIATAMENTE
      _downloadController.add(item);
    }
  }

  Future<String> _calculateChecksum(DownloadItem item) async {
    try {
      final home = Platform.environment['HOME']!;
      final filePath = '${home}/Downloads/${item.filename}';
      print('Calculating SHA-256 for: $filePath');
      
      final file = File(filePath);
      if (!await file.exists()) {
        throw Exception('File not found: $filePath');
      }
      
      final bytes = await file.readAsBytes();
      final digest = sha256.convert(bytes);
      
      // Agregar log inmediatamente
      item.addLog('SHA-256: ${digest.toString()}');
      _downloadController.add(item);
      
      return digest.toString();
    } catch (e) {
      print('Error calculating checksum: $e');
      return '';
    }
  }

  void _handleErrorMessage(Map<String, dynamic> data) {
    final url = data['url'];
    final error = data['error'];
    
    final item = _downloads[url];
    if (item != null) {
      item.status = DownloadStatus.error;
      item.error = error;
      _downloadController.add(item);
    }
    
    print('Download error: $error');
  }

  void _handleLogMessage(Map<String, dynamic> data) {
    final url = data['url'] as String;
    final message = data['message'] as String;
    
    final item = _downloads[url];
    if (item != null) {
      // Detectar y guardar SHA-256
      if (message.startsWith('SHA-256: ')) {
        item.checksum = message.substring(8);
      }
      
      item.addLog(message);
      _downloadController.add(item);
    }
  }
}
