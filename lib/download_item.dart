import 'download_service.dart'; // Solo para ChunkInfo

enum DownloadStatus { queued, downloading, completed, paused, error }

class DownloadItem {
  final String url;
  final String filename;
  int totalBytes; // Cambiar de final a int para permitir actualización
  int downloadedBytes;
  double progress;
  double currentSpeed;
  double averageSpeed;
  DownloadStatus status;
  String? tempStatus; // Para estados transitorios en UI (pausing, resuming)
  String? error;
  final DateTime startTime;
  String? checksum; // Añadir campo para guardar el SHA
  List<String> logs = []; // Añadir logs de la descarga

  // Añadir para rastrear último log de progreso
  DateTime? lastProgressLog;

  // Mapa para almacenar información de chunks
  final Map<int, dynamic> chunks = {};

  // Ajustes de configuración para velocidad y ETA
  static const int _maxSpeedHistory = 12; // Aumentado de 8 a 12
  static const double _etaAlpha =
      0.2; // Reducido de 0.3 a 0.2 para más suavidad
  static const Duration _speedUpdateInterval = Duration(milliseconds: 1000);

  // Variables para control de velocidad y ETA
  final List<double> _speedHistory = [];
  final List<double> _etaHistory = [];
  DateTime _lastSpeedUpdate = DateTime.now();
  double _smoothedSpeed = 0.0;
  double _lastEta = 0.0;

  // Estado UI para expandir/contraer log y chunks
  bool? expandLog;
  bool? expandChunks;

  // Campos para control de reintentos
  int? pauseRetries;
  int? resumeRetries;

  double lastLoggedProgress =
      0.0; // Add this field to track last logged progress

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
  String get formattedSize =>
      '${_formatBytes(downloadedBytes.toDouble())} / ${_formatBytes(totalBytes.toDouble())}';

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

    // Store ETA in history for smoothing
    _etaHistory.add(etaSeconds);
    if (_etaHistory.length > 8) {
      // Using implicit max length instead of const
      _etaHistory.removeAt(0);
    }

    // Calculate smoothed ETA using exponential moving average
    if (_etaHistory.length > 1) {
      // Use 0.3/0.7 ratio for more responsive updates
      _lastEta =
          _lastEta == 0
              ? etaSeconds
              : (_lastEta * (1 - _etaAlpha) + etaSeconds * _etaAlpha);
    } else {
      _lastEta = etaSeconds;
    }

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

    // Debug output to help identify duplicates
    //print("Adding log: $message");
    //print("Current logs: ${logs.length}");

    // Strict duplicate prevention: Check if exact message already exists
    for (final log in logs) {
      final logMessage = log.substring(log.indexOf(']') + 2);
      if (logMessage == message) {
        //print("Skipping duplicate: $message");
        return;
      }
    }

    // Special handling for percentages to avoid weird jumps
    if (message.startsWith('📥 ')) {
      final percentRegex = RegExp(r'(\d+\.\d+)%');
      final match = percentRegex.firstMatch(message);

      if (match != null) {
        final percent = double.tryParse(match.group(1) ?? '0');

        // For 99.9% - only add if we don't already have it
        if (percent == 99.9 && logs.any((log) => log.contains("99.9%"))) {
          return;
        }

        // For 100% - only add if we don't already have it
        if (percent == 100.0 && logs.any((log) => log.contains("100.0%"))) {
          return;
        }
      }
    }

    // Prevent multiple "Merging chunks" messages
    if (message.contains("Merging chunks") &&
        logs.any((log) => log.contains("Merging chunks"))) {
      return;
    }

    // Prevent multiple "completed successfully" messages
    if (message.contains("completed successfully") &&
        logs.any((log) => log.contains("completed successfully"))) {
      return;
    }

    // Prevent multiple checksum calculation messages
    if (message.startsWith('🔐') &&
        logs.any((log) => log.contains("Starting SHA-256"))) {
      return;
    }

    // Prevent multiple checksum result messages
    if (message.startsWith('🔑') &&
        logs.any((log) => log.contains("SHA-256 checksum:"))) {
      return;
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
    _smoothedSpeed =
        _smoothedSpeed == 0
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
      averageSpeed =
          _speedHistory.reduce((a, b) => a + b) / _speedHistory.length;
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
    }
  }

  // Agregar método para formatear el checksum en múltiples líneas
  String get formattedChecksum {
    if (checksum == null) return '';
    // Dividir el checksum en grupos de 32 caracteres
    final parts = <String>[];
    for (var i = 0; i < checksum!.length; i += 32) {
      final end = i + 32;
      parts.add(
        checksum!.substring(i, end > checksum!.length ? checksum!.length : end),
      );
    }
    return parts.join('\n');
  }
}
