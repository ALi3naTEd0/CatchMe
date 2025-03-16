class DownloadProgress {
  final String url;
  final int bytesReceived;
  final int totalBytes;
  final double speed;
  final String status;

  DownloadProgress({
    required this.url,
    required this.bytesReceived,
    required this.totalBytes,
    required this.speed,
    required this.status,
  });

  factory DownloadProgress.fromJson(Map<String, dynamic> json) {
    return DownloadProgress(
      url: json['url'] as String,
      bytesReceived: json['bytesReceived'] as int,
      totalBytes: json['totalBytes'] as int,
      speed: json['speed'] as double,
      status: json['status'] as String,
    );
  }
}
