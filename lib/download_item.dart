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

  // Historial de velocidades para calcular promedio
  static const int _maxHistoryEntries = 50;  // Aumentar historial
  final List<double> _speedHistory = [];
  DateTime _lastSpeedUpdate = DateTime.now();
  double _speedAccumulator = 0;
  int _speedSampleCount = 0;

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
  }) : startTime = DateTime.now();

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
    if (currentSpeed <= 0) return '--:--:--';
    final remaining = totalBytes - downloadedBytes;
    final seconds = remaining / currentSpeed;
    if (!seconds.isFinite) return '--:--:--';
    
    final duration = Duration(seconds: seconds.round());
    final hours = duration.inHours.toString().padLeft(2, '0');
    final minutes = (duration.inMinutes % 60).toString().padLeft(2, '0');
    final secs = (duration.inSeconds % 60).toString().padLeft(2, '0');
    return '$hours:$minutes:$secs';
  }

  // Agregar getter para elapsed time
  String get elapsed {
    final duration = DateTime.now().difference(startTime);
    final hours = duration.inHours.toString().padLeft(2, '0');
    final minutes = duration.inMinutes.toString().padLeft(2, '0');
    final secs = (duration.inSeconds % 60).toString().padLeft(2, '0');
    return '$hours:$minutes:$secs';
  }

  void addLog(String message) {
    final timestamp = DateTime.now();
    // Formatear hora con padding para asegurar siempre 2 dígitos
    final hours = timestamp.hour.toString().padLeft(2, '0');
    final minutes = timestamp.minute.toString().padLeft(2, '0');
    final seconds = timestamp.second.toString().padLeft(2, '0');
    final time = '$hours:$minutes:$seconds';
    
    // Limitar el tamaño de los logs para evitar problemas de memoria
    if (logs.length > 500) {
      logs.removeRange(0, 100); // Eliminar los 100 logs más antiguos
    }
    
    logs.add('[$time] $message');
  }

  // Método para actualizar velocidad actual y calcular promedio
  void updateSpeed(double newSpeed) {
    if (!newSpeed.isFinite || newSpeed < 0) return;

    // Actualizar velocidad actual
    currentSpeed = newSpeed;
    
    // Acumular para promedio móvil
    _speedAccumulator += newSpeed;
    _speedSampleCount++;
    
    // Actualizar promedio cada segundo
    final now = DateTime.now();
    if (now.difference(_lastSpeedUpdate) >= const Duration(seconds: 1)) {
      if (_speedSampleCount > 0) {
        final avgSpeed = _speedAccumulator / _speedSampleCount;
        _speedHistory.add(avgSpeed);
        if (_speedHistory.length > _maxHistoryEntries) {
          _speedHistory.removeAt(0);
        }
        
        // Calcular promedio general excluyendo valores extremos
        if (_speedHistory.length >= 3) {
          var speeds = List<double>.from(_speedHistory);
          speeds.sort();
          // Remover 10% superior e inferior
          final trimCount = (speeds.length * 0.1).round();
          if (trimCount > 0) {
            speeds = speeds.sublist(trimCount, speeds.length - trimCount);
          }
          averageSpeed = speeds.reduce((a, b) => a + b) / speeds.length;
        }
      }
      
      // Reiniciar acumuladores
      _speedAccumulator = 0;
      _speedSampleCount = 0;
      _lastSpeedUpdate = now;
    }
  }

  // Método simplificado para actualizar un chunk (no hacer cálculos aquí)
  void updateChunk(ChunkInfo chunkInfo) {
    // Simplemente guardar el chunk en el mapa
    chunks[chunkInfo.id] = chunkInfo;
  }
}