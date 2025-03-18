import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'dart:io';
import 'package:path_provider/path_provider.dart';
import 'download_item.dart';
import 'download_service.dart';

class CompletedScreen extends StatefulWidget {
  const CompletedScreen({super.key});

  @override
  State<CompletedScreen> createState() => _CompletedScreenState();
}

class _CompletedScreenState extends State<CompletedScreen> {
  // Crear una lista local de descargas completadas para mantenerlas
  final List<DownloadItem> _completedDownloads = [];
  final _downloadService = DownloadService();

  @override
  void initState() {
    super.initState();
    // Inicializar con descargas completadas existentes
    _updateCompletedDownloads();
    
    // Escuchar a nuevas descargas completadas
    _downloadService.downloadStream.listen((download) {
      if (download.status == DownloadStatus.completed) {
        _addCompletedDownload(download);
      }
    });
  }
  
  void _updateCompletedDownloads() {
    // Obtener descargas ya completadas del servicio
    final serviceCompleted = _downloadService.downloads
        .where((item) => item.status == DownloadStatus.completed)
        .toList();
        
    // Añadir solo las que no existan ya en nuestra lista
    for (final download in serviceCompleted) {
      _addCompletedDownload(download);
    }
  }
  
  void _addCompletedDownload(DownloadItem download) {
    if (!_completedDownloads.any((item) => item.url == download.url)) {
      setState(() {
        // Hacer una copia del item para que no se vea afectado por cambios futuros
        final completedDownload = DownloadItem(
          url: download.url,
          filename: download.filename,
          totalBytes: download.totalBytes,
          downloadedBytes: download.downloadedBytes,
          status: DownloadStatus.completed,
        );
        completedDownload.checksum = download.checksum;
        _completedDownloads.add(completedDownload);
      });
    }
  }
  
  void _removeCompletedDownload(DownloadItem download) {
    setState(() {
      _completedDownloads.removeWhere((item) => item.url == download.url);
    });
  }

  @override
  Widget build(BuildContext context) {
    if (_completedDownloads.isEmpty) {
      return Center(
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            Icon(Icons.check_circle_outline, size: 64, color: Colors.grey),
            SizedBox(height: 16),
            Text('No completed downloads'),
          ],
        ),
      );
    }

    return Scaffold(
      appBar: AppBar(
        backgroundColor: Theme.of(context).colorScheme.surface,
        title: const Text('Completed Downloads'),
      ),
      body: ListView.builder(
        itemCount: _completedDownloads.length,
        padding: EdgeInsets.all(16),
        itemBuilder: (context, index) {
          final download = _completedDownloads[index];
          return Card(
            child: ListTile(
              title: Text(download.filename),
              subtitle: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(download.formattedSize),
                  if (download.checksum != null)
                    Padding(
                      padding: const EdgeInsets.only(top: 4),
                      child: Row(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Icon(Icons.verified, size: 16, color: Colors.green),
                          SizedBox(width: 4),
                          Expanded(
                            child: Column(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                                Text(
                                  'SHA-256:',
                                  style: TextStyle(fontSize: 12, fontWeight: FontWeight.bold),
                                ),
                                Text(
                                  download.checksum!,
                                  style: TextStyle(
                                    fontSize: 11,
                                    fontFamily: 'monospace',
                                    letterSpacing: -0.3,
                                  ),
                                  softWrap: true,
                                ),
                              ],
                            ),
                          ),
                        ],
                      ),
                    ),
                ],
              ),
              trailing: Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  IconButton(
                    icon: Icon(Icons.folder_open),
                    onPressed: () => _openFileLocation(download),
                    tooltip: 'Open Downloads folder',
                  ),
                  if (download.checksum != null)
                    IconButton(
                      icon: Icon(Icons.copy),
                      onPressed: () => _copyChecksum(download),
                      tooltip: 'Copy checksum to clipboard',
                    ),
                  // Añadir botón para eliminar de la lista
                  IconButton(
                    icon: Icon(Icons.close, color: Colors.red[300]),
                    onPressed: () => _removeCompletedDownload(download),
                    tooltip: 'Remove from list',
                  ),
                ],
              ),
            ),
          );
        },
      ),
    );
  }

  void _openFileLocation(DownloadItem download) async {
    try {
      final home = Platform.environment['HOME']!;
      final path = '${home}/Downloads';
      print('Opening folder: $path');

      if (Platform.isLinux) {
        // Evitar bloquear la UI usando spawn
        final result = await Process.run('xdg-open', [path]);
        if (result.exitCode != 0) {
          throw Exception(result.stderr);
        }
      } else if (Platform.isWindows) {
        await Process.run('explorer.exe', [path]);
      } else if (Platform.isMacOS) {
        await Process.run('open', [path]);
      }
    } catch (e) {
      print('Error opening folder: $e');
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Could not open Downloads folder')),
        );
      }
    }
  }

  void _copyChecksum(DownloadItem download) {
    if (download.checksum != null) {
      Clipboard.setData(ClipboardData(text: download.checksum!));
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('Checksum copied to clipboard')),
      );
    }
  }
}
