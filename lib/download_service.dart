import 'dart:async';
import 'dart:io';
import 'dart:math';
import 'dart:convert';
import 'package:flutter/foundation.dart';  // Para compute()
import 'package:crypto/crypto.dart';       // Para sha256
import 'package:dio/dio.dart';
import 'package:path_provider/path_provider.dart';
import 'package:web_socket_channel/web_socket_channel.dart';
import 'download_item.dart';
import 'download_progress.dart';

class DownloadService {
  static final DownloadService _instance = DownloadService._internal();
  factory DownloadService() => _instance;
  DownloadService._internal();

  final _downloads = <String, DownloadItem>{};
  late StreamController<DownloadItem> _downloadController = StreamController<DownloadItem>.broadcast();
  final _dio = Dio();
  CancelToken? _cancelToken;
  bool _isPaused = false;
  final _socket = WebSocketChannel.connect(Uri.parse('ws://localhost:8080'));

  // Agregar propiedades para manejar el progreso
  final _downloadProgress = <String, int>{};
  final _lastUpdateTime = <String, DateTime>{};
  final _speedHistory = <String, List<double>>{};

  // Agregar para seguimiento de velocidad
  final _lastBytes = <String, int>{};
  final _lastTime = <String, DateTime>{};
  final _speedBuffer = <String, List<double>>{};
  static const _speedBufferSize = 5;

  static const _downloadTimeout = Duration(seconds: 30);
  int _maxRetries = 3;

  Stream<DownloadItem> get downloadStream => _downloadController.stream;
  List<DownloadItem> get downloads => _downloads.values.toList();

  void init() {
    if (_downloadController.isClosed) {
      _downloadController = StreamController<DownloadItem>.broadcast();
    }
    
    // Initialize WebSocket connection
    connectToServer();
  }

  Future<void> startDownload(String url, {int retryCount = 0}) async {
    try {
      print('Starting download: $url');
      
      if (!_isConnected) {
        await connectToServer();
      }
      
      // Create initial item for the UI
      final item = DownloadItem(
        url: url,
        filename: url.split('/').last,
        totalBytes: 0,
        status: DownloadStatus.downloading,
      );
      
      _downloads[url] = item;
      _downloadController.add(item);
      
      // Send download request to the Go server
      _channel!.sink.add(jsonEncode({
        'type': 'start_download',
        'url': url,
      }));

      await Future.any([
        _startActualDownload(url),
        Future.delayed(_downloadTimeout, () {
          throw TimeoutException('Download timed out');
        }),
      ]);

    } catch (e) {
      print('Download error: $e');
      if (retryCount < _maxRetries) {
        print('Retrying download ($retryCount/${_maxRetries})...');
        await Future.delayed(Duration(seconds: 2));
        await startDownload(url, retryCount: retryCount + 1);
      } else {
        final item = _downloads[url];
        if (item != null) {
          item.status = DownloadStatus.error;
          item.error = 'Failed after $_maxRetries retries: $e';
          _downloadController.add(item);
        }
      }
    }
  }

  Future<void> _startActualDownload(String url) async {
    // ...existing download logic...
  }

  String _getFilename(Response response) {
    final disposition = response.headers.value('content-disposition');
    if (disposition != null && disposition.contains('filename=')) {
      return disposition.split('filename=')[1].replaceAll('"', '');
    }
    final uri = Uri.parse(response.realUri.toString());
    return uri.pathSegments.last;
  }

  void pauseDownload(String url) {
    print('Pausing download: $url');
    
    if (_isConnected && _channel != null) {
      _channel!.sink.add(jsonEncode({
        'type': 'pause_download',
        'url': url,
      }));
      
      // Update UI immediately for better UX
      final item = _downloads[url];
      if (item != null) {
        item.status = DownloadStatus.paused;
        item.currentSpeed = 0;
        _downloadController.add(item);
      }
    }
  }

  void resumeDownload(String url) {
    print('Resuming download: $url');
    
    if (_isConnected && _channel != null) {
      _channel!.sink.add(jsonEncode({
        'type': 'resume_download',
        'url': url,
      }));
      
      // Update UI immediately for better UX
      final item = _downloads[url];
      if (item != null) {
        item.status = DownloadStatus.downloading;
        _downloadController.add(item);
      }
    }
  }

  void cancelDownload(String url) {
    print('Canceling download: $url');
    _cancelToken?.cancel('User cancelled download');
    final item = _downloads[url];
    if (item != null) {
      _downloads.remove(url);
      _downloadController.add(item..status = DownloadStatus.error);
    }
  }

  Stream<Map<String, dynamic>> get progressStream {
    return _socket.stream.map((event) {
      if (event is String) {
        return json.decode(event);
      }
      return event as Map<String, dynamic>;
    });
  }

  @override
  void dispose() {
    _downloadController.close();
  }

  // Improved WebSocket handling
  WebSocketChannel? _channel;
  bool _isConnected = false;
  Timer? _reconnectTimer;
  Timer? _pingTimer;

  Future<void> connectToServer() async {
    if (_isConnected) return;
    
    try {
      print('Connecting to WebSocket server...');
      _channel = WebSocketChannel.connect(Uri.parse('ws://localhost:8080/ws'));
      await _channel!.ready;
      
      _isConnected = true;
      print('Connected to server');
      
      // Setup listeners
      _setupMessageHandling();
      _startPingTimer();
      
    } catch (e) {
      print('Failed to connect to server: $e');
      _scheduleReconnect();
    }
  }

  void _setupMessageHandling() {
    _channel!.stream.listen(
      (message) => _handleServerMessage(message),
      onError: (e) {
        print('WebSocket error: $e');
        _handleDisconnect();
      },
      onDone: () {
        print('WebSocket connection closed');
        _handleDisconnect();
      },
    );
  }

  void _handleServerMessage(dynamic message) {
    if (message is! String) return;
    
    try {
      final data = jsonDecode(message);
      
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
          // Heartbeat response
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
      item.downloadedBytes = data['bytesReceived'] ?? 0;
      item.totalBytes = data['totalBytes'] ?? 0;
      
      if (item.totalBytes > 0) {
        item.progress = item.downloadedBytes / item.totalBytes;
      }
      
      item.currentSpeed = (data['speed'] ?? 0.0).toDouble();
      
      // Update average speed using exponential moving average
      if (item.averageSpeed <= 0) {
        item.averageSpeed = item.currentSpeed;
      } else {
        item.averageSpeed = (item.averageSpeed * 0.7) + (item.currentSpeed * 0.3);
      }
      
      // Añadir logs durante la descarga
      if (data['status'] == 'downloading') {
        final progress = (item.progress * 100).toStringAsFixed(1);
        item.addLog('Downloaded: $progress% at ${item.formattedSpeed}');
      }

      switch(data['status']) {
        case 'completed':
          item.status = DownloadStatus.completed;
          item.addLog('Download completed');
          
          // Calcular y mostrar checksum inmediatamente
          _calculateChecksum(item).then((checksum) {
            if (checksum.isNotEmpty) {
              item.checksum = checksum;
              item.addLog('File checksum (SHA-256): $checksum');
              _downloadController.add(item);
            }
          });
          break;
        case 'error':
          item.status = DownloadStatus.error;
          item.error = data['error'];
          break;
        case 'downloading':
          item.status = DownloadStatus.downloading;
          break;
        case 'paused':
          item.status = DownloadStatus.paused;
          break;
      }
      
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

  void _handleDisconnect() {
    _isConnected = false;
    _pingTimer?.cancel();
    _scheduleReconnect();
  }

  void _scheduleReconnect() {
    _reconnectTimer?.cancel();
    _reconnectTimer = Timer(Duration(seconds: 3), connectToServer);
  }

  void _startPingTimer() {
    _pingTimer?.cancel();
    _pingTimer = Timer.periodic(Duration(seconds: 30), (_) {
      if (_isConnected) {
        _channel!.sink.add(jsonEncode({'type': 'ping'}));
      }
    });
  }
}
