import 'package:flutter/material.dart';
import 'dart:io';
import 'package:path_provider/path_provider.dart';
import 'download_item.dart';
import 'download_service.dart';

class DownloadsScreen extends StatefulWidget {
  const DownloadsScreen({super.key});

  @override
  State<DownloadsScreen> createState() => _DownloadsScreenState();
}

class _DownloadsScreenState extends State<DownloadsScreen> {
  final _downloadService = DownloadService();
  final _urlController = TextEditingController();
  bool _serviceInitialized = false;

  @override
  void initState() {
    super.initState();
    // Inicialización retrasada para asegurar que el servidor esté listo
    WidgetsBinding.instance.addPostFrameCallback((_) {
      _initService();
    });
  }
  
  Future<void> _initService() async {
    // Esperar un poco para asegurar que el servidor esté listo
    await Future.delayed(const Duration(milliseconds: 800));
    await _downloadService.init();
    setState(() {
      _serviceInitialized = true;
    });
  }

  void _showAddDownloadDialog(BuildContext context) {
    showDialog(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('New Download'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            TextField(
              controller: _urlController,
              decoration: const InputDecoration(
                labelText: 'URL',
                hintText: 'https://example.com/file.zip',
                border: OutlineInputBorder(),
              ),
              autofocus: true,
              onSubmitted: (url) {
                Navigator.of(context).pop();
                _startDownload(context, url);
              },
            ),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () {
              final url = _urlController.text;
              if (url.isNotEmpty) {
                Navigator.of(context).pop();
                _startDownload(context, url);
              }
            },
            child: const Text('Download'),
          ),
        ],
      ),
    );
  }

  void _startDownload(BuildContext context, String url) {
    print('Attempting to start download: $url');
    _downloadService.startDownload(url);
  }

  @override
  void dispose() {
    _urlController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        backgroundColor: Theme.of(context).colorScheme.surface,
        title: const Text('Downloads'),
        actions: [
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 16.0),
            child: FilledButton.icon(
              icon: const Icon(Icons.add),
              label: const Text('New Download'),
              onPressed: () => _showAddDownloadDialog(context),
            ),
          ),
        ],
      ),
      body: !_serviceInitialized 
        ? const Center(
            child: CircularProgressIndicator(),
          )
        : StreamBuilder<DownloadItem>(
          stream: _downloadService.downloadStream,
          builder: (context, snapshot) {
            // Mostrar lista aunque no haya datos en el stream
            return ListView.builder(
              itemCount: _downloadService.downloads.length,
              padding: const EdgeInsets.all(16),
              itemBuilder: (context, index) {
                final download = _downloadService.downloads[index];
                return _buildDownloadCard(download);
              },
            );
          },
        ),
    );
  }

  Widget _buildDownloadCard(DownloadItem download) {
    // Definir el color con un valor por defecto para evitar nulls
    final accent = Colors.blue[300] ?? Colors.blue;

    return Card(
      margin: const EdgeInsets.only(bottom: 16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          // Header con filename y controles
          Padding(
            padding: const EdgeInsets.all(16),
            child: Row(
              children: [
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        download.filename,
                        style: Theme.of(context).textTheme.titleMedium,
                      ),
                      Text(
                        download.url,
                        style: TextStyle(fontSize: 12, color: Colors.grey[400]),
                      ),
                    ],
                  ),
                ),
                if (download.status == DownloadStatus.completed)
                  IconButton(
                    icon: Icon(Icons.folder_open, color: accent),
                    onPressed: () => _openFileLocation(download),
                  )
                else
                  IconButton(
                    icon: Icon(
                      download.status == DownloadStatus.downloading 
                        ? Icons.pause : Icons.play_arrow,
                      color: accent,
                    ),
                    onPressed: () => _toggleDownload(download),
                  ),
                IconButton(
                  icon: Icon(Icons.close, color: Colors.red[300]),
                  onPressed: () => _downloadService.cancelDownload(download.url),
                ),
              ],
            ),
          ),

          // Barra de progreso con stats
          Container(
            padding: const EdgeInsets.fromLTRB(16, 0, 16, 16),
            child: Column(
              children: [
                // Progreso y Size
                Row(
                  mainAxisAlignment: MainAxisAlignment.spaceBetween,
                  children: [
                    Text(
                      download.formattedProgress,
                      style: TextStyle(
                        fontSize: 16,
                        fontWeight: FontWeight.bold,
                        color: accent,
                      ),
                    ),
                    Text(
                      download.formattedSize,
                      style: TextStyle(color: accent),
                    ),
                  ],
                ),
                const SizedBox(height: 8),

                // Barra de progreso
                LinearProgressIndicator(
                  value: download.progress,
                  backgroundColor: Colors.grey[800],
                  valueColor: AlwaysStoppedAnimation<Color>(accent), // Ahora accent no es nullable
                  minHeight: 6,
                ),
                const SizedBox(height: 12),

                // Stats en dos columnas con mejor contraste
                Row(
                  children: [
                    // Columna izquierda: Velocidades
                    Expanded(
                      child: Row(
                        children: [
                          _buildStat(
                            icon: Icons.speed,
                            label: 'Speed:',
                            value: download.formattedSpeed,
                            color: accent,  // Ahora accent no es nullable
                          ),
                          const SizedBox(width: 24),
                          _buildStat(
                            icon: Icons.show_chart,
                            label: 'Avg:',
                            value: download.formattedAvgSpeed,
                            color: accent,
                          ),
                        ],
                      ),
                    ),
                    // Columna derecha: Tiempos
                    Expanded(
                      child: Row(
                        mainAxisAlignment: MainAxisAlignment.end,
                        children: [
                          _buildStat(
                            icon: Icons.timer,
                            label: 'ETA:',
                            value: download.eta,
                            color: accent,
                          ),
                          const SizedBox(width: 24),
                          _buildStat(
                            icon: Icons.access_time,
                            label: 'Time:',
                            value: download.elapsed,
                            color: accent,
                          ),
                        ],
                      ),
                    ),
                  ],
                ),
              ],
            ),
          ),

          // Mini log más visible y durante la descarga
          if (download.status == DownloadStatus.downloading || download.logs.isNotEmpty)
            Container(
              margin: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
              padding: const EdgeInsets.all(8),
              decoration: BoxDecoration(
                color: Colors.grey[900],
                borderRadius: BorderRadius.circular(4),
                border: Border.all(color: accent.withOpacity(0.2)),
              ),
              constraints: BoxConstraints(
                maxHeight: 120,  // Más alto para mostrar más logs
                minHeight: 60,   // Altura mínima para que siempre se vea bien
              ),
              child: ListView.builder(
                reverse: true,
                itemCount: download.logs.length,
                itemBuilder: (_, i) => Padding(
                  padding: const EdgeInsets.symmetric(vertical: 2),
                  child: Text(
                    download.logs[download.logs.length - i - 1],
                    style: TextStyle(
                      fontFamily: 'monospace',
                      fontSize: 11,
                      color: Colors.grey[400],
                    ),
                  ),
                ),
              ),
            ),
        ],
      ),
    );
  }

  Widget _buildStat({
    required IconData icon,
    required String label,
    required String value,
    required Color color,
  }) {
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Icon(icon, size: 16, color: color),
        const SizedBox(width: 4),
        Text(
          label,
          style: TextStyle(color: color),
        ),
        const SizedBox(width: 4),
        Text(value),
      ],
    );
  }

  Widget _buildHeader(DownloadItem download) {
    return Padding(
      padding: const EdgeInsets.all(16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          // URL con estilo monoespacio
          Text(
            download.url,
            style: TextStyle(
              fontFamily: 'monospace',
              fontSize: 12,
              color: Theme.of(context).colorScheme.onSurface.withOpacity(0.6),
            ),
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
          ),
          const SizedBox(height: 8),
          
          // Filename and controls
          Row(
            children: [
              Expanded(
                child: Text(
                  download.filename,
                  style: Theme.of(context).textTheme.titleMedium,
                ),
              ),
              if (download.status == DownloadStatus.completed)
                IconButton(
                  icon: Icon(
                    Icons.folder_open,
                    color: Theme.of(context).colorScheme.primary,
                  ),
                  onPressed: () => _openFileLocation(download),
                  tooltip: 'Open folder',
                )
              else
                IconButton(
                  icon: Icon(
                    download.status == DownloadStatus.downloading 
                      ? Icons.pause 
                      : Icons.play_arrow,
                    color: Theme.of(context).colorScheme.primary,
                  ),
                  onPressed: () => _toggleDownload(download),
                ),
              IconButton(
                icon: Icon(
                  Icons.close,
                  color: Theme.of(context).colorScheme.error,
                ),
                onPressed: () => _downloadService.cancelDownload(download.url),
              ),
            ],
          ),
        ],
      ),
    );
  }

  Widget _buildStatItem({
    required IconData icon,
    required String label,
    required String value,
  }) {
    return Container(
      padding: EdgeInsets.symmetric(horizontal: 8),
      child: Column(
        children: [
          Icon(icon, size: 16, color: Theme.of(context).colorScheme.primary),
          const SizedBox(height: 4),
          Text(label,
            style: TextStyle(
              fontSize: 12,
              color: Theme.of(context).colorScheme.primary,
            ),
          ),
          Text(value,
            style: TextStyle(fontSize: 12),
          ),
        ],
      ),
    );
  }

  Widget _buildRetryButton(DownloadItem download) {
    return TextButton.icon(
      onPressed: () => _retryDownload(download),
      icon: Icon(Icons.refresh),
      label: Text('Retry'),
      style: TextButton.styleFrom(
        foregroundColor: Theme.of(context).colorScheme.error,
      ),
    );
  }

  void _retryDownload(DownloadItem download) {
    _downloadService.startDownload(download.url);
  }

  Future<void> _openFileLocation(DownloadItem download) async {
    try {
      // Usar la ruta ~/Downloads directamente, no la ruta de documentos
      final home = Platform.environment['HOME']!;
      final downloadsPath = '$home/Downloads';
      print('Opening downloads folder: $downloadsPath');
      
      if (Platform.isLinux) {
        final result = await Process.run('xdg-open', [downloadsPath]);
        if (result.exitCode != 0) {
          throw Exception('Failed to open Downloads folder');
        }
      } else if (Platform.isWindows) {
        await Process.run('explorer.exe', [downloadsPath.replaceAll('/', '\\')]);
      } else if (Platform.isMacOS) {
        await Process.run('open', [downloadsPath]);
      }
    } catch (e) {
      print('Error opening folder: $e');
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('Could not open Downloads folder: $e')),
      );
    }
  }

  void _toggleDownload(DownloadItem download) {
    if (download.status == DownloadStatus.downloading) {
      _downloadService.pauseDownload(download.url);
    } else {
      _downloadService.resumeDownload(download.url);
    }
  }
}
