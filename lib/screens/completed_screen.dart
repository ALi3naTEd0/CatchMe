import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:path/path.dart' as p;

import '../core/catchme_client.dart';
import '../core/download.dart';

/// Finished downloads, newest first.
class CompletedScreen extends StatelessWidget {
  const CompletedScreen({super.key, required this.client});
  final CatchMeClient client;

  static Future<void> showInFolder(String path) async {
    if (Platform.isMacOS) {
      await Process.run('open', ['-R', path]);
    } else if (Platform.isWindows) {
      await Process.run('explorer', ['/select,', path]);
    } else {
      await Process.run('xdg-open', [p.dirname(path)]);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.all(24),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text('Completed', style: Theme.of(context).textTheme.headlineSmall),
          const SizedBox(height: 16),
          Expanded(
            child: ListenableBuilder(
              listenable: client,
              builder: (context, _) {
                final items =
                    client.downloads.where((d) => d.status == DownloadStatus.completed).toList()
                      ..sort((a, b) => (b.finishedAt ?? b.addedAt).compareTo(a.finishedAt ?? a.addedAt));
                if (items.isEmpty) return const Center(child: Text('Finished downloads will appear here.'));
                return ListView.separated(
                  itemCount: items.length,
                  separatorBuilder: (_, _) => const SizedBox(height: 8),
                  itemBuilder: (context, i) => _CompletedCard(d: items[i], client: client),
                );
              },
            ),
          ),
        ],
      ),
    );
  }
}

class _CompletedCard extends StatelessWidget {
  const _CompletedCard({required this.d, required this.client});
  final Download d;
  final CatchMeClient client;

  @override
  Widget build(BuildContext context) {
    final finished = d.finishedAt?.toLocal();
    final when =
        finished == null
            ? ''
            : '${finished.year}-${finished.month.toString().padLeft(2, '0')}-${finished.day.toString().padLeft(2, '0')} '
                '${finished.hour.toString().padLeft(2, '0')}:${finished.minute.toString().padLeft(2, '0')}';
    return Card(
      child: ListTile(
        leading: const Icon(Icons.check_circle, color: Colors.green),
        title: Text(d.displayName, overflow: TextOverflow.ellipsis),
        subtitle: Text(
          [
            formatBytes(d.total),
            when,
            if (d.sha256 != null) 'SHA-256 ${d.sha256!.substring(0, 12)}…',
          ].where((s) => s.isNotEmpty).join(' · '),
        ),
        trailing: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            if (d.sha256 != null)
              IconButton(
                icon: const Icon(Icons.fingerprint),
                tooltip: 'Copy SHA-256',
                onPressed: () async {
                  await Clipboard.setData(ClipboardData(text: d.sha256!));
                  if (context.mounted) {
                    ScaffoldMessenger.of(context).showSnackBar(const SnackBar(content: Text('SHA-256 copied')));
                  }
                },
              ),
            if (d.path != null)
              IconButton(
                icon: const Icon(Icons.folder_open),
                tooltip: 'Show in folder',
                onPressed: () => CompletedScreen.showInFolder(d.path!),
              ),
            IconButton(
              icon: const Icon(Icons.playlist_remove),
              tooltip: 'Remove from list (keeps the file)',
              onPressed: () async {
                try {
                  await client.remove(d.id);
                } on CommandException catch (e) {
                  if (context.mounted) ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(e.message)));
                }
              },
            ),
          ],
        ),
      ),
    );
  }
}
