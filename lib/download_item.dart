import 'package:flutter/material.dart';
import 'dart:math';
import 'download_service.dart';  // Solo para ChunkInfo

enum DownloadStatus { queued, downloading, completed, paused, error }

class DownloadItem {
  final String url;
  final String filename;
  int totalBytes;  // Cambiar de final a int para permitir actualización
  int downloadedBytes;
  double progress;
  double currentSpeed;
  double averageSpeed;
  DownloadStatus status;
  String? tempStatus;  // Para estados transitorios en UI (pausing, resuming)
  String? error;
  final DateTime startTime;
  String? checksum;  // Añadir campo para guardar el SHA
  List<String> logs = [];  // Añadir logs de la descarga
  
  // Añadir para rastrear último log de progreso
  DateTime? lastProgressLog;

  // Mapa para almacenar información de chunks
  final Map<int, dynamic> chunks = {};

  // Ajustes de configuración para velocidad y ETA
  static const int _maxSpeedHistory = 12;      // Aumentado de 8 a 12
  static const int _maxEtaHistory = 8;         // Aumentado de 3 a 8
  static const double _etaAlpha = 0.2;         // Reducido de 0.3 a 0.2 para más suavidad
  static const double _speedAlpha = 0.1;       // Reducido de 0.15 a 0.1
  static const Duration _speedUpdateInterval = Duration(milliseconds: 1000);
  
  // Variables para control de velocidad y ETA
  final List<double> _speedHistory = [];
  final List<double> _etaHistory = [];
  DateTime _lastSpeedUpdate = DateTime.now();
  double _speedAccumulator = 0;
  int _speedSampleCount = 0;
  double _smoothedSpeed = 0.0;
  double _lastEta = 0.0;

  // Estado UI para expandir/contraer log y chunks
  bool? expandLog;
  bool? expandChunks;
  
  // Campos para control de reintentos
  int? pauseRetries;
  int? resumeRetries;

  // Modificar constructor para mantener chunks al completar
  DownloadItem({
    required this.url,
    required this.filename,
    required this.totalBytes,
    this.downloadedBytes = 0,
    this.progress = 0.0,
    this.currentSpeed = 0.0,
    this.averageSpeed = 0.0,
    this.status = DownloadStatus.queued,
    this.tempStatus,
    this.error,
    this.expandLog = true,
    this.expandChunks = false,
  }) : startTime = DateTime.now() {
    // Inicializar mapa de chunks vacío pero no nulo
    chunks.clear();
  }

  String get formattedProgress => '${(progress * 100).toStringAsFixed(1)}%';
  String get formattedSpeed => _formatSpeed(currentSpeed);
  String get formattedAvgSpeed => _formatSpeed(averageSpeed);
  String get formattedSize => '${_formatBytes(downloadedBytes.toDouble())} / ${_formatBytes(totalBytes.toDouble())}';
  
  String _formatSpeed(double bytesPerSecond) {
    if (!bytesPerSecond.isFinite || bytesPerSecond <= 0) return '0 B/s';
    return '${_formatBytes(bytesPerSecond)}/s';
  }

  static String _formatBytes(double bytes) {
    if (!bytes.isFinite || bytes <= 0) return '0 B';
    const suffixes = ['B', 'KB', 'MB', 'GB', 'TB'];
    var i = 0;
    while (bytes >= 1024 && i < suffixes.length - 1) {
      bytes /= 1024;
      i++;
    }
    return '${bytes.toStringAsFixed(1)} ${suffixes[i]}';
  }

  String get eta {
    if (currentSpeed <= 0 || !currentSpeed.isFinite) return '--:--:--';
    
    final remaining = totalBytes - downloadedBytes;
    if (remaining <= 0) return '00:00:00';

    // More responsive ETA calculation
    final etaSeconds = remaining / currentSpeed;
    if (!etaSeconds.isFinite || etaSeconds <= 0) return '--:--:--';

    // Use 0.3/0.7 ratio for more responsive updates
    _lastEta = _lastEta == 0 ? etaSeconds : (_lastEta * 0.3 + etaSeconds * 0.7);

    final duration = Duration(seconds: _lastEta.round());
    
    // Formatear sin exceder 99:59:59
    int hours = duration.inHours.clamp(0, 99);
    int minutes = (duration.inMinutes % 60).clamp(0, 59);
    int seconds = (duration.inSeconds % 60).clamp(0, 59);
    
    return '${hours.toString().padLeft(2, '0')}:'
           '${minutes.toString().padLeft(2, '0')}:'
           '${seconds.toString().padLeft(2, '0')}';
  }

  // Agregar getter para elapsed time
  String get elapsed {
    final duration = DateTime.now().difference(startTime);
    final hours = duration.inHours.toString().padLeft(2, '0');
    final minutes = (duration.inMinutes % 60).toString().padLeft(2, '0');
    final secs = (duration.inSeconds % 60).toString().padLeft(2, '0');
    return '$hours:$minutes:$secs';
  }

  void addLog(String message) {
    final timestamp = DateTime.now();
    final hours = timestamp.hour.toString().padLeft(2, '0');
    final minutes = timestamp.minute.toString().padLeft(2, '0');
    final seconds = timestamp.second.toString().padLeft(2, '0');
    final time = '$hours:$minutes:$seconds';
    
    // Evitar duplicados consecutivos
    if (logs.isNotEmpty) {
      final lastLog = logs.last;
      final lastMsgStart = lastLog.indexOf(']') + 2;
      if (lastMsgStart > 0 && lastMsgStart < lastLog.length) {
        final lastMsg = lastLog.substring(lastMsgStart);
        
        // Si el mensaje es idéntico al último, no lo añadir
        if (lastMsg == message) {
          return;
        }
        
        // Manejar incrementos de progreso
        if (message.startsWith('📥 ') && lastMsg.startsWith('📥 ')) {
          final percentRegex = RegExp(r'(\d+\.\d+)%');
          final lastMatch = percentRegex.firstMatch(lastMsg);
          final currentMatch = percentRegex.firstMatch(message);
          
          if (lastMatch != null && currentMatch != null) {
            final lastPercent = double.tryParse(lastMatch.group(1) ?? '0');
            final currentPercent = double.tryParse(currentMatch.group(1) ?? '0');
            
            if (lastPercent != null && currentPercent != null) {
              // Solo permitir incrementos de 0.1%
              final difference = currentPercent - lastPercent;
              if (difference < 0.1) {
                return; // Ignorar cambios menores a 0.1%
              }
              
              // Asegurar incrementos consecutivos
              final expectedPercent = (lastPercent * 10).round() / 10 + 0.1;
              if (currentPercent > expectedPercent) {
                // Generar incrementos intermedios
                var nextPercent = expectedPercent;
                while (nextPercent < currentPercent) {
                  logs.add('[$time] 📥 ${nextPercent.toStringAsFixed(1)}%');
                  nextPercent += 0.1;
                }
                return; // El porcentaje actual se añadirá en la siguiente actualización
              }
            }
          }
        }
      }
    }
    
    // Limitar el tamaño de los logs
    if (logs.length > 500) {
      logs.removeRange(0, 100);
    }
    
    logs.add('[$time] $message');
  }

  // Método para actualizar velocidad actual y calcular promedio
  void updateSpeed(double newSpeed) {
    if (!newSpeed.isFinite || newSpeed < 0) return;

    final now = DateTime.now();
    if (now.difference(_lastSpeedUpdate) < _speedUpdateInterval) {
      return; // Ignorar actualizaciones demasiado frecuentes
    }
    _lastSpeedUpdate = now;

    // Implementar suavizado exponencial con alfa más bajo
    const alpha = 0.15; // Reducir factor de suavizado para más estabilidad
    _smoothedSpeed = _smoothedSpeed == 0 
        ? newSpeed 
        : (_smoothedSpeed * (1 - alpha) + newSpeed * alpha);

    currentSpeed = _smoothedSpeed;
    
    // Mantener historial más corto
    _speedHistory.add(_smoothedSpeed);
    if (_speedHistory.length > _maxSpeedHistory) {
      _speedHistory.removeAt(0);
    }
    
    // Calcular promedio móvil para velocidad promedio
    if (_speedHistory.length >= 3) {
      averageSpeed = _speedHistory.reduce((a, b) => a + b) / _speedHistory.length;
    }
  }

  // Método simplificado para actualizar un chunk (no hacer cálculos aquí)
  void updateChunk(ChunkInfo chunkInfo) {
    // Simplemente guardar el chunk en el mapa
    chunks[chunkInfo.id] = chunkInfo;
  }

  // Agregar un helper para detectar estados transitorios
  bool get isInTransition => tempStatus != null;

  String get statusDisplay {
    if (tempStatus == 'pausing') return 'Pausing...';
    if (tempStatus == 'resuming') return 'Resuming...';
    
    switch (status) {
      case DownloadStatus.queued:
        return 'Queued';
      case DownloadStatus.downloading:
        return 'Downloading';
      case DownloadStatus.completed:
        return 'Completed';
      case DownloadStatus.paused:
        return 'Paused';
      case DownloadStatus.error:
        return 'Error';
      default:
        return 'Unknown';
    }
  }

  String get statusText {
    if (tempStatus == 'pausing') return 'Pausing...';
    if (tempStatus == 'resuming') return 'Resuming...';
    
    switch (status) {
      case DownloadStatus.queued:
        return 'Queued';
      case DownloadStatus.downloading:
        return 'Downloading';
      case DownloadStatus.completed:
        return 'Completed';
      case DownloadStatus.paused:
        return 'Paused';
      case DownloadStatus.error:
        return 'Error';
      default:
        return 'Unknown';
    }
  }

  // Agregar método para formatear el checksum en múltiples líneas
  String get formattedChecksum {
    if (checksum == null) return '';
    // Dividir el checksum en grupos de 32 caracteres
    final parts = <String>[];
    for (var i = 0; i < checksum!.length; i += 32) {
      final end = i + 32;
      parts.add(checksum!.substring(i, end > checksum!.length ? checksum!.length : end));
    }
    return parts.join('\n');
  }
}