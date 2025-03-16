import 'package:flutter/material.dart';
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

  @override
  void initState() {
    super.initState();
    _downloadService.init(); // Asegurar que el stream está listo
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
      body: StreamBuilder<DownloadItem>(
        stream: _downloadService.downloadStream,
        builder: (context, snapshot) {
          // Mostrar lista aunque no haya datos en el stream
          return ListView.builder(
            itemCount: _downloadService.downloads.length,
            padding: const EdgeInsets.all(16),
            itemBuilder: (context, index) {
              final download = _downloadService.downloads[index];
              return Card(
                margin: const EdgeInsets.only(bottom: 16),
                child: Padding(
                  padding: const EdgeInsets.all(16),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      // URL
                      Text(
                        download.url,
                        style: Theme.of(context).textTheme.bodySmall?.copyWith(
                          color: Colors.grey[400],
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
                          IconButton(
                            icon: Icon(
                              download.status == DownloadStatus.downloading 
                                ? Icons.pause 
                                : Icons.play_arrow,
                              color: Theme.of(context).colorScheme.primary,
                            ),
                            onPressed: () => download.status == DownloadStatus.downloading
                              ? _downloadService.pauseDownload(download.url)
                              : _downloadService.resumeDownload(download.url),
                          ),
                          IconButton(
                            icon: Icon(Icons.close, 
                              color: Theme.of(context).colorScheme.error
                            ),
                            onPressed: () => _downloadService.cancelDownload(download.url),
                          ),
                        ],
                      ),
                      const SizedBox(height: 12),
                      // Stats row 1 - Progress, Speed y Avg juntos
                      Row(
                        mainAxisAlignment: MainAxisAlignment.spaceBetween,
                        children: [
                          Text(
                            '${download.formattedProgress}',
                            style: Theme.of(context).textTheme.titleMedium?.copyWith(
                              fontWeight: FontWeight.bold,
                            ),
                          ),
                          if (download.currentSpeed > 0) ...[
                            Text('↓ ${download.formattedSpeed}'),
                            Text('⌀ ${download.formattedAvgSpeed}'),
                          ],
                        ],
                      ),
                      const SizedBox(height: 8),
                      // Progress bar
                      Container(
                        height: 12,
                        decoration: BoxDecoration(
                          borderRadius: BorderRadius.circular(6),
                          color: Theme.of(context).colorScheme.surface,
                        ),
                        child: ClipRRect(
                          borderRadius: BorderRadius.circular(6),
                          child: LinearProgressIndicator(
                            value: download.progress,
                            backgroundColor: Colors.transparent,
                            valueColor: AlwaysStoppedAnimation<Color>(
                              Theme.of(context).colorScheme.primary,
                            ),
                          ),
                        ),
                      ),
                      const SizedBox(height: 8),
                      // Stats row 2 - Ahora size, eta y elapsed
                      Row(
                        mainAxisAlignment: MainAxisAlignment.spaceBetween,
                        children: [
                          Text(download.formattedSize),
                          Text('ETA: ${download.eta}'),
                          Text('Time: ${download.elapsed}'),
                        ],
                      ),
                    ],
                  ),
                ),
              );
            },
          );
        },
      ),
    );
  }
}
