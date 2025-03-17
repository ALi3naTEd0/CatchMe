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
  final _downloadService = DownloadService();

  @override
  Widget build(BuildContext context) {
    // Filtrar solo descargas completadas
    final completedDownloads = _downloadService.downloads
        .where((item) => item.status == DownloadStatus.completed)
        .toList();

    if (completedDownloads.isEmpty) {
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

    return ListView.builder(
      itemCount: completedDownloads.length,
      padding: EdgeInsets.all(16),
      itemBuilder: (context, index) {
        final download = completedDownloads[index];
        return Card(
          child: ListTile(
            title: Text(download.filename),
            subtitle: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(download.formattedSize),
                if (download.checksum != null)
                  Row(
                    children: [
                      Icon(Icons.verified, size: 16, color: Colors.green),
                      SizedBox(width: 4),
                      Text(
                        'SHA-256: ${download.checksum!.substring(0, 8)}...',
                        style: TextStyle(fontSize: 12),
                      ),
                    ],
                  ),
              ],
            ),
            trailing: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                IconButton(
                  icon: Icon(Icons.folder_open),
                  onPressed: () => _openFileLocation(download),
                ),
                IconButton(
                  icon: Icon(Icons.copy),
                  onPressed: () => _copyChecksum(download),
                ),
              ],
            ),
          ),
        );
      },
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
