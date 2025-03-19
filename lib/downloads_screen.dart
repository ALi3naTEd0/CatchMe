import 'package:flutter/material.dart';
import 'package:flutter/services.dart'; // Agregar esta línea para Clipboard
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
            // Importante: No usar Key única con DateTime pues fuerza reconstrucción
            // causando lag y problemas de rendimiento
            
            // Imprimir información para depuración
            if (snapshot.hasData) {
              final download = snapshot.data!;
              print('UI received update: ${download.filename} - ${download.formattedProgress}');
            }
            
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
    
    // Estado para controlar la expansión del log y chunks
    bool expandLog = download.expandLog ?? false;
    bool expandChunks = download.expandChunks ?? false;
    
    // Determinar qué icono mostrar basado en el estado y el estado temporal
    IconData actionIcon;
    bool enableButton = true;
    
    // Lógica para el icono de acción basado en el estado + estado temporal
    if (download.tempStatus == 'pausing') {
      actionIcon = Icons.hourglass_top;
      enableButton = false;  // Deshabilitar botón mientras se procesa
    } else if (download.tempStatus == 'resuming') {
      actionIcon = Icons.hourglass_bottom;
      enableButton = false;  // Deshabilitar botón mientras se procesa
    } else if (download.status == DownloadStatus.downloading) {
      actionIcon = Icons.pause;
    } else if (download.status == DownloadStatus.paused) {
      actionIcon = Icons.play_arrow;
    } else if (download.status == DownloadStatus.completed) {
      actionIcon = Icons.folder_open;
    } else {
      actionIcon = Icons.pause;  // Valor por defecto
    }

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
                      Row(
                        children: [
                          Expanded(
                            child: Text(
                              download.url,
                              style: TextStyle(fontSize: 12, color: Colors.grey[400]),
                              overflow: TextOverflow.ellipsis,
                            ),
                          ),
                          // Mostrar el estado aquí, especialmente para pausing/resuming
                          if (download.tempStatus != null)
                            Text(
                              download.statusDisplay,
                              style: TextStyle(
                                fontSize: 12, 
                                color: download.tempStatus == 'pausing' ? Colors.orange : Colors.green,
                                fontWeight: FontWeight.bold
                              ),
                            ),
                        ],
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
                    icon: Icon(actionIcon, color: accent),
                    onPressed: enableButton ? () => _toggleDownload(download) : null,
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
                              
                // Mini log con toggle para expandir/contraer
                if (download.logs.isNotEmpty)
                  AnimatedContainer(
                    duration: Duration(milliseconds: 200),
                    margin: const EdgeInsets.only(top: 16),
                    decoration: BoxDecoration(
                      color: Colors.grey[900],
                      borderRadius: BorderRadius.circular(4),
                      border: Border.all(color: accent.withOpacity(0.2)),
                    ),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        InkWell(
                          onTap: () {
                            setState(() {
                              download.expandLog = !expandLog;
                            });
                          },
                          child: Padding(
                            padding: const EdgeInsets.all(8.0),
                            child: Row(
                              mainAxisAlignment: MainAxisAlignment.spaceBetween,
                              children: [
                                Text(
                                  'Download Log',
                                  style: TextStyle(fontWeight: FontWeight.bold),
                                ),
                                Row(
                                  children: [
                                    IconButton(
                                      icon: const Icon(Icons.copy, size: 16),
                                      onPressed: () => _copyLogs(download),
                                      tooltip: 'Copy logs',
                                    ),
                                    Icon(
                                      expandLog ? Icons.keyboard_arrow_up : Icons.keyboard_arrow_down,
                                      color: accent,
                                    ),
                                  ],
                                ),
                              ],
                            ),
                          ),
                        ),
                        
                        // Logs que aparecen solo cuando está expandido
                        AnimatedContainer(
                          duration: Duration(milliseconds: 200),
                          height: expandLog ? 150 : 0,
                          curve: Curves.easeInOut,
                          child: Container(
                            constraints: BoxConstraints(
                              minHeight: expandLog ? 150 : 0,
                              maxHeight: expandLog ? 150 : 0,
                            ),
                            child: ListView.builder(
                              reverse: true,
                              itemCount: download.logs.length,
                              padding: EdgeInsets.all(8),
                              itemExtent: 18.0,
                              itemBuilder: (_, i) => SelectableText(
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
                      ],
                    ),
                  ),

                // Chunks siempre visibles pero colapsables
                if (download.chunks.isNotEmpty)
                  AnimatedContainer(
                    duration: Duration(milliseconds: 200),
                    margin: const EdgeInsets.only(top: 16),
                    decoration: BoxDecoration(
                      color: Colors.grey[900],
                      borderRadius: BorderRadius.circular(4),
                      border: Border.all(color: accent.withOpacity(0.3)),
                    ),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        InkWell(
                          onTap: () {
                            setState(() {
                              download.expandChunks = !expandChunks;
                            });
                          },
                          child: Padding(
                            padding: const EdgeInsets.all(8.0),
                            child: Row(
                              mainAxisAlignment: MainAxisAlignment.spaceBetween,
                              children: [
                                Text(
                                  'Download Chunks (${download.chunks.length})',
                                  style: TextStyle(fontWeight: FontWeight.bold),
                                ),
                                Row(
                                  children: [
                                    Text(
                                      '${download.chunks.values.where((c) => c.status == 'completed').length}/${download.chunks.length}',
                                      style: TextStyle(fontSize: 12, color: Colors.grey[400]),
                                    ),
                                    IconButton(
                                      icon: const Icon(Icons.copy, size: 16),
                                      onPressed: () => _copyChunksInfo(download),
                                      tooltip: 'Copy chunks info',
                                    ),
                                    Icon(
                                      expandChunks ? Icons.keyboard_arrow_up : Icons.keyboard_arrow_down,
                                      color: accent,
                                    ),
                                  ],
                                ),
                              ],
                            ),
                          ),
                        ),
                        
                        // Chunks que aparecen solo cuando está expandido
                        AnimatedContainer(
                          duration: Duration(milliseconds: 200),
                          height: expandChunks ? null : 48,
                          curve: Curves.easeInOut,
                          child: SingleChildScrollView(
                            padding: EdgeInsets.all(8),
                            physics: expandChunks ? AlwaysScrollableScrollPhysics() : NeverScrollableScrollPhysics(),
                            child: Column(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              mainAxisSize: MainAxisSize.min,
                              children: [
                                if (expandChunks)
                                  ..._buildExpandedChunksContent(download, accent)
                                else
                                  _buildCollapsedChunksContent(download, accent),
                              ],
                            ),
                          ),
                        ),
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
    // Verificar si la descarga ya está en proceso de pausa/resumen
    if (download.isInTransition) {
      print('Download already in transition state: ${download.tempStatus}');
      return;
    }
    
    if (download.status == DownloadStatus.completed) {
      _openFileLocation(download);
      return;
    }

    if (download.status == DownloadStatus.downloading) {
      print('UI: Pausing download: ${download.url}');
      _downloadService.pauseDownload(download.url);
    } else if (download.status == DownloadStatus.paused) {
      print('UI: Resuming download: ${download.url}');
      _downloadService.resumeDownload(download.url);
    } else {
      print('Download in non-toggleable state: ${download.status}');
    }
  }

  // Agregar métodos para copiar información
  void _copyLogs(DownloadItem download) {
    final logs = download.logs.join('\n');
    Clipboard.setData(ClipboardData(text: logs));
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text('Logs copied to clipboard')),
    );
  }

  void _copyChunksInfo(DownloadItem download) {
    final buffer = StringBuffer();
    buffer.writeln('Chunks status for ${download.filename}:');
    buffer.writeln('Total chunks: ${download.chunks.length}');
    buffer.writeln('Completed: ${download.chunks.values.where((c) => c.status == 'completed').length}');
    buffer.writeln('\nDetailed chunks info:');
    
    final sortedChunks = download.chunks.values.toList()
      ..sort((a, b) => a.id.compareTo(b.id));
    
    for (var chunk in sortedChunks) {
      buffer.writeln('Chunk #${chunk.id + 1}:');
      buffer.writeln('  Status: ${chunk.status}');
      buffer.writeln('  Progress: ${(chunk.progressPercentage * 100).toStringAsFixed(1)}%');
      buffer.writeln('  Size: ${_formatBytes((chunk.end - chunk.start + 1).toDouble())}');
    }

    Clipboard.setData(ClipboardData(text: buffer.toString()));
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text('Chunks info copied to clipboard')),
    );
  }

  // Nuevos métodos auxiliares para la UI de chunks
  List<Widget> _buildExpandedChunksContent(DownloadItem download, Color accent) {
    final sections = [
      ('active', 'Active Chunks', accent),
      ('paused', 'Paused Chunks', Colors.orange),
      ('pending', 'Pending Chunks', Colors.grey),
      ('completed', 'Completed Chunks', Colors.green),
      ('failed', 'Failed Chunks', Colors.red),
    ];
    
    List<Widget> widgets = [];
    
    for (final (status, title, color) in sections) {
      final chunks = download.chunks.values.where((c) => c.status == status).toList();
      if (chunks.isNotEmpty) {
        widgets.add(
          Padding(
            padding: const EdgeInsets.only(top: 8, bottom: 4),
            child: Text(title, style: TextStyle(fontSize: 12, color: color)),
          )
        );
        widgets.addAll(chunks.map((c) => _buildChunkProgressItem(c, accent)));
      }
    }
    
    return widgets;
  }

  Widget _buildCollapsedChunksContent(DownloadItem download, Color accent) {
    return Row(
      children: [
        Expanded(
          child: LinearProgressIndicator(
            value: download.progress,
            backgroundColor: Colors.grey[800],
            valueColor: AlwaysStoppedAnimation<Color>(accent),
            minHeight: 8,
          ),
        ),
        SizedBox(width: 8),
        Text(
          '${download.chunks.values.where((c) => c.status == 'active').length} active',
          style: TextStyle(fontSize: 10),
        ),
      ],
    );
  }

  // Agregar método _formatBytes que faltaba
  String _formatBytes(double bytes) {
    if (bytes <= 0) return '0 B';
    const suffixes = ['B', 'KB', 'MB', 'GB', 'TB'];
    var i = 0;
    while (bytes >= 1024 && i < suffixes.length - 1) {
      bytes /= 1024;
      i++;
    }
    return '${bytes.toStringAsFixed(1)} ${suffixes[i]}';
  }
}
