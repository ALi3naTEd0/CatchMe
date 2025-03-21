import 'package:flutter/material.dart';
import 'package:flutter/services.dart'; // Para Clipboard
import 'dart:async'; // Para Timer
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
  Timer? _clipboardCheckTimer;
  
  // Mapa para mantener un ScrollController por cada URL
  final Map<String, ScrollController> _scrollControllers = {};
  
  // Set para rastrear URLs ya procesadas
  final Set<String> _clipboardProcessedUrls = {};

  @override
  void initState() {
    super.initState();
    // Inicialización retrasada para asegurar que el servidor esté listo
    WidgetsBinding.instance.addPostFrameCallback((_) {
      _initService();
      _startClipboardMonitoring();
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

  // Verificar clipboard periódicamente
  void _startClipboardMonitoring() {
    _clipboardCheckTimer = Timer.periodic(Duration(seconds: 2), (_) {
      _checkClipboardForUrl();
    });
  }

  // Verificar si hay una URL en el clipboard
  Future<void> _checkClipboardForUrl() async {
    try {
      final data = await Clipboard.getData(Clipboard.kTextPlain);
      if (data != null && data.text != null) {
        final text = data.text!;
        if (_isValidUrl(text) && !_clipboardProcessedUrls.contains(text)) {
          _clipboardProcessedUrls.add(text); // Evitar procesar la misma URL múltiples veces
          _showClipboardUrlNotification(text);
        }
      }
    } catch (e) {
      print('Error checking clipboard: $e');
    }
  }

  // Verificar si es una URL válida
  bool _isValidUrl(String text) {
    return text.startsWith('http://') || text.startsWith('https://');
  }

  // Mostrar notificación para URL detectada
  void _showClipboardUrlNotification(String url) {
    if (!mounted) return;
    
    WidgetsBinding.instance.addPostFrameCallback((_) {
      _urlController.text = url;
      _showAddDownloadDialog(context);
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
                _startDownload(context, url, true); // Siempre usar chunks por defecto
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
                _startDownload(context, url, true); // Siempre usar chunks por defecto
              }
            },
            child: const Text('Download'),
          ),
        ],
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
    _clipboardCheckTimer?.cancel();
    
    // Limpiar todos los ScrollControllers
    for (final controller in _scrollControllers.values) {
      controller.dispose();
    }
    _scrollControllers.clear();
    _urlController.dispose();
    super.dispose();
  }

  // Obtener o crear un ScrollController para una URL
  ScrollController _getScrollController(String url) {
    if (!_scrollControllers.containsKey(url)) {
      _scrollControllers[url] = ScrollController();
    }
    
    // Programar un scroll al final después de la construcción
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (_scrollControllers[url]!.hasClients) {
        _scrollControllers[url]!.animateTo(
          _scrollControllers[url]!.position.maxScrollExtent,
          duration: Duration(milliseconds: 300),
          curve: Curves.easeOut,
        );
      }
    });
    
    return _scrollControllers[url]!;
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
    
    // Add action icon and button logic
    final actionIcon = download.status == DownloadStatus.downloading 
        ? Icons.pause
        : download.status == DownloadStatus.paused 
            ? Icons.play_arrow 
            : Icons.folder_open;
            
    final enableButton = download.status == DownloadStatus.downloading ||
                        download.status == DownloadStatus.paused ||
                        download.status == DownloadStatus.completed;

    // Clean up layout variables
    final screenWidth = MediaQuery.of(context).size.width;
    final isMobile = screenWidth < 800;
    
    // Get current states
    bool expandLog = download.expandLog ?? false;
    bool expandChunks = download.expandChunks ?? false;
    
    return Card(
      margin: const EdgeInsets.only(bottom: 16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          // Header with filename and controls
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
          
          // Progress section with speed stats
          Container(
            padding: const EdgeInsets.fromLTRB(16, 0, 16, 16),
            child: Column(
              children: [
                // Progress and Size
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
                
                // Progress bar
                LinearProgressIndicator(
                  value: download.progress.clamp(0.0, 1.0), // Ensure progress is clamped
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
                              
                // Mini log y chunks section
                Container(
                  padding: const EdgeInsets.fromLTRB(16, 0, 16, 16),
                  child: Column(
                    children: [
                      // Solo mostrar Log y Chunks, quitar sección de SHA-256
                      if (download.logs.isNotEmpty)
                        _buildLogSection(download, expandLog, accent),
                      
                      if (download.chunks.isNotEmpty)
                        _buildChunksSection(download, expandChunks, accent),
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

  // Métodos auxiliares para la UI de chunks
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

  Widget _buildLogSection(DownloadItem download, bool expandLog, Color accent) {
    // Usar un ValueListenableBuilder para evitar reconstruir todo el widget
    return AnimatedContainer(
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
                  Icon(
                    expandLog ? Icons.keyboard_arrow_up : Icons.keyboard_arrow_down,
                    color: accent,
                  ),
                ],
              ),
            ),
          ),
          
          // Usar un RepaintBoundary para aislar las actualizaciones del log
          RepaintBoundary(
            child: AnimatedContainer(
              duration: Duration(milliseconds: 200),
              height: expandLog ? 150 : 0,
              child: Container(
                constraints: BoxConstraints(
                  minHeight: expandLog ? 150 : 0,
                  maxHeight: expandLog ? 150 : 0,
                ),
                child: ListView.builder(
                  controller: _getScrollController(download.url),
                  itemCount: download.logs.length,
                  padding: EdgeInsets.all(8),
                  // Usar cacheExtent para mantener más items en memoria
                  cacheExtent: 20,
                  itemExtent: 18.0,
                  itemBuilder: (_, i) {
                    final log = download.logs[i];
                    return SingleChildScrollView(
                      scrollDirection: Axis.horizontal,
                      child: Text(
                        log,
                        style: const TextStyle(
                          fontFamily: 'monospace',
                          fontSize: 11,
                          color: Colors.white70,
                          height: 1.2,
                        ),
                      ),
                    );
                  },
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildChunksSection(DownloadItem download, bool expandChunks, Color accent) {
    return AnimatedContainer(
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
                download.expandChunks = !expandChunks;
              });
            },
            child: Padding(
              padding: const EdgeInsets.all(8.0),
              child: Row(
                mainAxisAlignment: MainAxisAlignment.spaceBetween,
                children: [
                  Text(
                    'Download Chunks',
                    style: TextStyle(fontWeight: FontWeight.bold),
                  ),
                  Row(
                    children: [
                      Text(
                        '${download.chunks.values.where((c) => c.status == 'completed').length}/${download.chunks.length}',
                        style: TextStyle(fontSize: 12, color: Colors.grey[400]),
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
          
          AnimatedContainer(
            duration: Duration(milliseconds: 200),
            height: expandChunks ? null : 0,
            child: expandChunks 
              ? Padding(
                  padding: EdgeInsets.all(8),
                  child: Column(
                    children: _buildExpandedChunksContent(download, accent),
                  ),
                )
              : const SizedBox.shrink(),
          ),
        ],
      ),
    );
  }
}
