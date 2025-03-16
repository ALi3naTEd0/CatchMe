import 'dart:async';
import 'dart:io';
import 'dart:math';
import 'dart:convert';
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

  Stream<DownloadItem> get downloadStream => _downloadController.stream;
  List<DownloadItem> get downloads => _downloads.values.toList();

  void init() {
    if (_downloadController.isClosed) {
      _downloadController = StreamController<DownloadItem>.broadcast();
    }
  }

  Future<void> startDownload(String url) async {
    try {
      print('-------------- Starting new download --------------');
      print('URL: $url');
      print('Stream controller state: ${_downloadController.isClosed ? 'closed' : 'open'}');

      if (_downloadController.isClosed) {
        print('Reinitializing stream controller');
        init();
      }

      // Verificar si ya existe
      if (_downloads.containsKey(url)) {
        print('Download already exists, current state:');
        print('Status: ${_downloads[url]?.status}');
        print('Progress: ${_downloads[url]?.progress}');
        return;
      }

      print('Starting download: $url');
      _cancelToken = CancelToken();
      _isPaused = false;

      // Enviar petición al servidor Go
      _socket.sink.add({
        'type': 'start_download',
        'url': url,
      });

      // Get file info
      final response = await _dio.head(url);
      final contentLength = int.parse(response.headers.value('content-length') ?? '0');
      final filename = _getFilename(response);

      // Create download item
      final item = DownloadItem(
        url: url,
        filename: filename,
        totalBytes: contentLength,
      );
      _downloads[url] = item;

      // Get download directory
      final dir = await getApplicationDocumentsDirectory();
      final savePath = '${dir.path}/downloads/$filename';

      // Create directory if not exists
      await Directory('${dir.path}/downloads').create(recursive: true);

      // Si es una reanudación, usar el progreso guardado
      final existingProgress = _downloadProgress[url] ?? 0;
      if (existingProgress > 0) {
        print('Resuming from byte: $existingProgress');
        _dio.options.headers['Range'] = 'bytes=$existingProgress-';
      }

      // Start download
      await _dio.download(
        url,
        savePath,
        cancelToken: _cancelToken,
        onReceiveProgress: (received, total) {
          if (_isPaused) return;
          
          // Garantizar números finitos y positivos
          if (received < 0 || total <= 0) {
            print('Invalid progress values: received=$received, total=$total');
            return;
          }

          try {
            final now = DateTime.now();
            final elapsed = max(now.difference(item.startTime).inSeconds, 1);
            
            item.downloadedBytes = received;
            item.progress = received / total;
            
            // Prevenir división por cero y valores inválidos
            if (elapsed > 0) {
              // Velocidad instantánea con límite máximo
              final speed = min(received / elapsed, received.toDouble());
              item.currentSpeed = speed.isFinite ? speed : 0;
              
              // Media móvil exponencial para velocidad promedio
              if (item.averageSpeed <= 0) {
                item.averageSpeed = item.currentSpeed;
              } else {
                item.averageSpeed = (item.averageSpeed * 0.7) + (item.currentSpeed * 0.3);
              }
            } else {
              item.currentSpeed = 0;
              item.averageSpeed = 0;
            }

            item.status = DownloadStatus.downloading;
            _downloadController.add(item);
          } catch (e) {
            print('Error calculating speeds: $e');
          }
        },
        deleteOnError: false,
      );

      item.status = DownloadStatus.completed;
      _downloadController.add(item);
    } catch (e, stack) {
      print('Download error:');
      print('Error: $e');
      print('Stack: $stack');

      final item = _downloads[url];
      if (item != null) {
        item.status = DownloadStatus.error;
        item.error = e.toString();
        try {
          _downloadController.add(item);
        } catch (streamError) {
          print('Error updating stream:');
          print('Error: $streamError');
        }
      }
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

  void pauseDownload(String url) {
    print('Pausing download: $url');
    _isPaused = true;
    // Preservar progreso actual
    _lastBytes[url] = _downloads[url]?.downloadedBytes ?? 0;
    _cancelToken?.cancel('Paused by user');
    _cancelToken = null;
    
    final download = _downloads[url];
    if (download != null) {
      download.status = DownloadStatus.paused;
      download.currentSpeed = 0;
      download.averageSpeed = 0;
      _downloadController.add(download);
    }
  }

  void resumeDownload(String url) {
    print('Resuming download: $url');
    _isPaused = false;
    final item = _downloads[url];
    if (item != null) {
      startDownload(url); // Reiniciar descarga desde el último punto
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
}
