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
    bool useChunks = true; // por defecto activado
    
    showDialog(
      context: context,
      builder: (context) => StatefulBuilder(
        builder: (context, setState) => AlertDialog(
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
                  _startDownload(context, url, useChunks);
                },
              ),
              const SizedBox(height: 16),
              // Opción para usar chunks
              SwitchListTile(
                title: const Text('Use chunked download'),
                subtitle: Text(
                  'Download file in multiple parallel connections',
                  style: TextStyle(fontSize: 12),
                ),
                value: useChunks,
                onChanged: (value) {
                  setState(() {
                    useChunks = value;
                  });
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
                  _startDownload(context, url, useChunks);
                }
              },
              child: const Text('Download'),
            ),
          ],
        ),
      ),
    );
  }

  void _startDownload(BuildContext context, String url, [bool useChunks = true]) {
    print('Attempting to start download: $url (Chunks: $useChunks)');
    try {
      _downloadService.startDownload(url, useChunks: useChunks).catchError((error) {
        // Mostrar error en SnackBar para mejorar UX
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Error: ${error.toString()}')),
        );
      });
    } catch (e) {
      print('Error starting download: $e');
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('Error: $e')),
      );
    }
    
    // Limpiar campo después de iniciar descarga
    _urlController.clear();
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
    final accent = Colors.blue[300] ?? Colors.blue;
    final screenWidth = MediaQuery.of(context).size.width;
    final isMobile = screenWidth < 800;

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
                  valueColor: AlwaysStoppedAnimation<Color>(accent),
                  minHeight: 6,
                ),
                const SizedBox(height: 12),

                // Stats según layout
                if (isMobile)
                  Column(
                    children: [
                      // Primera fila: Speed y ETA
                      Row(
                        children: [
                          Expanded(
                            flex: 1,
                            child: _buildStat(
                              icon: Icons.speed,
                              label: 'Speed:',
                              value: download.formattedSpeed,
                              color: accent,
                              iconSize: 18,
                              fontSize: 13,
                            ),
                          ),
                          Expanded(
                            flex: 1,
                            child: Align(
                              alignment: Alignment.centerRight,
                              child: _buildStat(
                                icon: Icons.timer,
                                label: 'ETA:',
                                value: download.eta,
                                color: accent,
                                iconSize: 18,
                                fontSize: 13,
                              ),
                            ),
                          ),
                        ],
                      ),
                      const SizedBox(height: 8),
                      // Segunda fila: Avg y Time
                      Row(
                        children: [
                          Expanded(
                            flex: 1,
                            child: _buildStat(
                              icon: Icons.show_chart,
                              label: 'Avg:',
                              value: download.formattedAvgSpeed,
                              color: accent,
                              iconSize: 18,
                              fontSize: 13,
                            ),
                          ),
                          Expanded(
                            flex: 1,
                            child: Align(
                              alignment: Alignment.centerRight,
                              child: _buildStat(
                                icon: Icons.access_time,
                                label: 'Time:',
                                value: download.elapsed,
                                color: accent,
                                iconSize: 18,
                                fontSize: 13,
                              ),
                            ),
                          ),
                        ],
                      ),
                    ],
                  )
                else
                  Row(
                    children: [
                      Expanded(
                        child: Row(
                          children: [
                            _buildStat(
                              icon: Icons.speed,
                              label: 'Speed:',
                              value: download.formattedSpeed,
                              color: accent,
                              iconSize: 18,
                              fontSize: 13,
                            ),
                            const SizedBox(width: 24),
                            _buildStat(
                              icon: Icons.show_chart,
                              label: 'Avg:',
                              value: download.formattedAvgSpeed,
                              color: accent,
                              iconSize: 18,
                              fontSize: 13,
                            ),
                          ],
                        ),
                      ),
                      Expanded(
                        child: Row(
                          mainAxisAlignment: MainAxisAlignment.end,
                          children: [
                            _buildStat(
                              icon: Icons.timer,
                              label: 'ETA:',
                              value: download.eta,
                              color: accent,
                              iconSize: 18,
                              fontSize: 13,
                            ),
                            const SizedBox(width: 24),
                            _buildStat(
                              icon: Icons.access_time,
                              label: 'Time:',
                              value: download.elapsed,
                              color: accent,
                              iconSize: 18,
                              fontSize: 13,
                            ),
                          ],
                        ),
                      ),
                    ],
                  ),

                // Mini log más visible y durante la descarga
                if (download.status == DownloadStatus.downloading || download.logs.isNotEmpty)
                  Container(
                    margin: const EdgeInsets.only(top: 16),
                    padding: const EdgeInsets.all(8),
                    decoration: BoxDecoration(
                      color: Colors.grey[900],
                      borderRadius: BorderRadius.circular(4),
                      border: Border.all(color: accent.withOpacity(0.2)),
                    ),
                    constraints: const BoxConstraints(
                      maxHeight: 150,  // Más alto para mostrar más logs
                      minHeight: 80,  // Altura mínima para que siempre se vea bien
                    ),
                    child: download.logs.isEmpty 
                      ? Center(
                          child: Text(
                            'No logs available',
                            style: TextStyle(color: Colors.grey[600], fontStyle: FontStyle.italic),
                          ),
                        )
                      : ListView.builder(
                          reverse: true,
                          itemCount: download.logs.length,
                          padding: EdgeInsets.zero,
                          itemExtent: 18.0, // Altura fija por elemento para scroll uniforme
                          itemBuilder: (_, i) => Padding(
                            padding: const EdgeInsets.symmetric(vertical: 1),
                            child: Text(
                              download.logs[download.logs.length - i - 1],
                              style: const TextStyle(
                                fontFamily: 'monospace',
                                fontSize: 11,
                                color: Colors.white70,
                                height: 1.2,
                              ),
                            ),
                          ),
                        ),
                  ),
                
                // Visualización mejorada de chunks - SIEMPRE mostrar si hay chunks, no solo durante la descarga
                if (download.chunks.isNotEmpty)
                  Container(
                    margin: const EdgeInsets.only(top: 16),
                    padding: const EdgeInsets.all(8),
                    decoration: BoxDecoration(
                      color: Colors.grey[900],
                      borderRadius: BorderRadius.circular(4),
                      border: Border.all(color: accent.withOpacity(0.2)),
                    ),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Padding(
                          padding: const EdgeInsets.only(bottom: 8),
                          child: Text(
                            'Download Chunks (${download.chunks.length})',
                            style: TextStyle(fontWeight: FontWeight.bold, fontSize: 14),
                          ),
                        ),
                        // Mostrar chunks con más actividad - Ordenar por estado, mostrando activos primero
                        ...download.chunks.values.toList()
                          .where((c) => c.status != 'completed') // Mostrar primero chunks no completados
                          .take(3) // Mostrar máximo 3 chunks activos
                          .map((chunk) => _buildChunkProgressItem(chunk, accent))
                          .followedBy(download.chunks.values.toList()
                            .where((c) => c.status == 'completed') // Luego mostrar completados
                            .take(2) // Mostrar máximo 2 chunks completados
                            .map((chunk) => _buildChunkProgressItem(chunk, accent)))
                          .take(5) // Limitar a total 5 chunks
                          .toList(),
                      ],
                    ),
                  ),
              ],
            ),
          ),
        ],
      ),
    );
  }

  // Extraer widget para chunk para hacerlo más limpio
  Widget _buildChunkProgressItem(dynamic chunk, Color accent) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 4),
      child: Row(
        children: [
          Container(
            width: 24,
            child: Text('#${chunk.id + 1}', style: TextStyle(fontSize: 10)),
          ),
          Expanded(
            child: LinearProgressIndicator(
              value: chunk.progressPercentage,
              backgroundColor: Colors.grey[800],
              valueColor: AlwaysStoppedAnimation<Color>(_getChunkColor(chunk.status, accent)),
              minHeight: 8,
            ),
          ),
          SizedBox(width: 8),
          Text(
            _getChunkStatusText(chunk),
            style: TextStyle(fontSize: 10),
          ),
        ],
      ),
    );
  }

  Color _getChunkColor(String status, Color defaultColor) {
    switch (status) {
      case 'pending':
        return Colors.grey;
      case 'active':
        return defaultColor;
      case 'completed':
        return Colors.green;
      case 'failed':
        return Colors.red;
      case 'paused':
        return Colors.orange;
      default:
        return defaultColor;
    }
  }

  String _getChunkStatusText(ChunkInfo chunk) {
    switch (chunk.status) {
      case 'pending':
        return 'Pending';
      case 'active':
        final progress = ((chunk.progressPercentage) * 100).toStringAsFixed(0);
        return '$progress% @ ${_formatSpeed(chunk.speed)}';
      case 'completed':
        return 'Complete';
      case 'failed':
        return 'Failed';
      case 'paused':
        return 'Paused';
      default:
        return chunk.status;
    }
  }

  String _formatSpeed(double bytesPerSecond) {
    if (bytesPerSecond <= 0) return '0 B/s';
    
    const units = ['B/s', 'KB/s', 'MB/s', 'GB/s'];
    int unitIndex = 0;
    double value = bytesPerSecond;
    
    while (value > 1024 && unitIndex < units.length - 1) {
      value /= 1024;
      unitIndex++;
    }
    
    return '${value.toStringAsFixed(1)} ${units[unitIndex]}';
  }

  Widget _buildStat({
    required IconData icon,
    required String label,
    required String value,
    required Color color,
    double iconSize = 18,
    double fontSize = 13,
  }) {
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Icon(icon, size: iconSize, color: color),
        SizedBox(width: iconSize * 0.25),
        Text(
          label,
          style: TextStyle(
            color: color,
            fontSize: fontSize,
          ),
        ),
        SizedBox(width: iconSize * 0.25),
        Text(
          value,
          style: TextStyle(fontSize: fontSize),
        ),
      ],
    );
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
      print('UI: Pausing download: ${download.url}');
      _downloadService.pauseDownload(download.url);
    } else if (download.status == DownloadStatus.paused) {
      print('UI: Resuming download: ${download.url}');
      _downloadService.resumeDownload(download.url);
    }
  }
}
