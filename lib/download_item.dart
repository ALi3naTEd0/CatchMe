import 'package:flutter/material.dart';
import 'dart:math';

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
  String? error;
  final DateTime startTime;
  String? checksum;  // Añadir campo para guardar el SHA
  List<String> logs = [];  // Añadir logs de la descarga

  // Historial de velocidades para calcular promedio
  final List<double> _speedHistory = [];
  static const int _maxHistoryEntries = 10;

  DownloadItem({
    required this.url,
    required this.filename,
    required this.totalBytes,
    this.downloadedBytes = 0,
    this.progress = 0.0,
    this.currentSpeed = 0.0,
    this.averageSpeed = 0.0,
    this.status = DownloadStatus.queued,
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
    final minutes = (duration.inMinutes % 60).toString().padLeft(2, '0');
    final secs = (duration.inSeconds % 60).toString().padLeft(2, '0');
    return '$hours:$minutes:$secs';
  }

  void addLog(String message) {
    final timestamp = DateTime.now();
    final time = '${timestamp.hour}:${timestamp.minute}:${timestamp.second}';
    logs.add('[$time] $message');
  }

  // Método para actualizar velocidad actual y calcular promedio
  void updateSpeed(double newSpeed) {
    if (newSpeed.isFinite && newSpeed >= 0) {
      currentSpeed = newSpeed;
      
      // Agregar al historial limitando tamaño
      _speedHistory.add(newSpeed);
      if (_speedHistory.length > _maxHistoryEntries) {
        _speedHistory.removeAt(0);
      }
      
      // Calcular promedio solo si hay datos
      if (_speedHistory.isNotEmpty) {
        averageSpeed = _speedHistory.reduce((a, b) => a + b) / _speedHistory.length;
      }
    }
  }
}