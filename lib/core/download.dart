/// A download as the daemon reports it (the Go `download.Snapshot`).
library;

enum DownloadStatus { queued, running, paused, completed, failed, unknown }

DownloadStatus _status(String? s) => switch (s) {
  'queued' => DownloadStatus.queued,
  'running' => DownloadStatus.running,
  'paused' => DownloadStatus.paused,
  'completed' => DownloadStatus.completed,
  'failed' => DownloadStatus.failed,
  _ => DownloadStatus.unknown,
};

class Download {
  const Download({
    required this.id,
    required this.url,
    required this.status,
    required this.done,
    required this.total,
    required this.speed,
    required this.addedAt,
    this.name,
    this.path,
    this.sha256,
    this.error,
    this.finishedAt,
  });

  final String id;
  final String url;
  final String? name;
  final String? path;
  final DownloadStatus status;
  final int done;
  final int total; // -1 while unknown
  final double speed; // bytes per second
  final String? sha256;
  final String? error;
  final DateTime addedAt;
  final DateTime? finishedAt;

  factory Download.fromJson(Map<String, dynamic> j) => Download(
    id: j['id'] as String,
    url: j['url'] as String,
    name: j['name'] as String?,
    path: j['path'] as String?,
    status: _status(j['status'] as String?),
    done: (j['done'] as num?)?.toInt() ?? 0,
    total: (j['total'] as num?)?.toInt() ?? -1,
    speed: (j['speed'] as num?)?.toDouble() ?? 0,
    sha256: j['sha256'] as String?,
    error: j['error'] as String?,
    addedAt: DateTime.tryParse(j['addedAt'] as String? ?? '') ?? DateTime.now(),
    finishedAt: DateTime.tryParse(j['finishedAt'] as String? ?? ''),
  );

  /// The file name, or the last part of the URL until the daemon knows it.
  String get displayName {
    if (name != null && name!.isNotEmpty) return name!;
    final segments = Uri.tryParse(url)?.pathSegments.where((s) => s.isNotEmpty);
    return (segments == null || segments.isEmpty) ? url : segments.last; // already decoded
  }

  /// 0..1, or null when the size is unknown.
  double? get fraction => total > 0 ? (done / total).clamp(0, 1).toDouble() : null;

  Duration? get eta {
    if (status != DownloadStatus.running || total <= 0 || speed < 1) return null;
    return Duration(seconds: ((total - done) / speed).ceil());
  }

  bool get isActive => status == DownloadStatus.queued || status == DownloadStatus.running;
}

String formatBytes(num bytes) {
  if (bytes < 0) return '?';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  var value = bytes.toDouble();
  var unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit++;
  }
  return '${value.toStringAsFixed(unit == 0 || value >= 100 ? 0 : 1)} ${units[unit]}';
}

String formatSpeed(double bytesPerSecond) => '${formatBytes(bytesPerSecond)}/s';

String formatDuration(Duration d) {
  if (d.inHours > 0) return '${d.inHours} h ${d.inMinutes.remainder(60)} min';
  if (d.inMinutes > 0) return '${d.inMinutes} min ${d.inSeconds.remainder(60)} s';
  return '${d.inSeconds} s';
}
