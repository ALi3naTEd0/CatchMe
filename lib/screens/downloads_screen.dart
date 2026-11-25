import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../core/catchme_client.dart';
import '../core/download.dart';

/// Downloads that are not finished: queued, running, paused or failed.
class DownloadsScreen extends StatefulWidget {
  const DownloadsScreen({super.key, required this.client});
  final CatchMeClient client;

  @override
  State<DownloadsScreen> createState() => _DownloadsScreenState();
}

class _DownloadsScreenState extends State<DownloadsScreen> {
  final _url = TextEditingController();
  bool _adding = false;

  @override
  void dispose() {
    _url.dispose();
    super.dispose();
  }

  Future<void> _add() async {
    final url = _url.text.trim();
    if (url.isEmpty) return;
    setState(() => _adding = true);
    try {
      await widget.client.add(url);
      _url.clear();
    } on CommandException catch (e) {
      _toast(e.code == 'bad_url' ? 'Only http and https links can be downloaded.' : e.message);
    } finally {
      if (mounted) setState(() => _adding = false);
    }
  }

  Future<void> _paste() async {
    final data = await Clipboard.getData(Clipboard.kTextPlain);
    if (data?.text != null) _url.text = data!.text!.trim();
  }

  Future<void> _run(Future<void> Function() action) async {
    try {
      await action();
    } on CommandException catch (e) {
      _toast(e.message);
    }
  }

  void _toast(String msg) {
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(msg)));
  }

  Future<void> _confirmCancel(Download d) async {
    final ok = await showDialog<bool>(
      context: context,
      builder:
          (ctx) => AlertDialog(
            title: const Text('Cancel download?'),
            content: Text('"${d.displayName}" will stop and what was downloaded so far will be deleted.'),
            actions: [
              TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('Keep downloading')),
              FilledButton(onPressed: () => Navigator.pop(ctx, true), child: const Text('Cancel download')),
            ],
          ),
    );
    if (ok == true) await _run(() => widget.client.cancel(d.id));
  }

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.all(24),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text('Downloads', style: Theme.of(context).textTheme.headlineSmall),
          const SizedBox(height: 16),
          Row(
            children: [
              Expanded(
                child: TextField(
                  controller: _url,
                  decoration: InputDecoration(
                    hintText: 'Paste a link (http or https)',
                    border: const OutlineInputBorder(),
                    suffixIcon: IconButton(icon: const Icon(Icons.content_paste), tooltip: 'Paste', onPressed: _paste),
                  ),
                  onSubmitted: (_) => _add(),
                ),
              ),
              const SizedBox(width: 12),
              FilledButton.icon(
                onPressed: _adding ? null : _add,
                icon: const Icon(Icons.download),
                label: const Text('Download'),
              ),
            ],
          ),
          const SizedBox(height: 24),
          Expanded(
            child: ListenableBuilder(
              listenable: widget.client,
              builder: (context, _) {
                final items =
                    widget.client.downloads
                        .where((d) => d.status != DownloadStatus.completed)
                        .toList()
                        .reversed
                        .toList();
                if (items.isEmpty) {
                  return const Center(child: Text('Nothing downloading. Paste a link above to start.'));
                }
                return ListView.separated(
                  itemCount: items.length,
                  separatorBuilder: (_, _) => const SizedBox(height: 8),
                  itemBuilder:
                      (context, i) => _DownloadCard(
                        d: items[i],
                        onPause: () => _run(() => widget.client.pause(items[i].id)),
                        onResume: () => _run(() => widget.client.resume(items[i].id)),
                        onCancel: () => _confirmCancel(items[i]),
                        onRemove: () => _run(() => widget.client.remove(items[i].id)),
                      ),
                );
              },
            ),
          ),
        ],
      ),
    );
  }
}

class _DownloadCard extends StatelessWidget {
  const _DownloadCard({
    required this.d,
    required this.onPause,
    required this.onResume,
    required this.onCancel,
    required this.onRemove,
  });

  final Download d;
  final VoidCallback onPause, onResume, onCancel, onRemove;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final (label, color) = switch (d.status) {
      DownloadStatus.queued => ('Waiting', scheme.outline),
      DownloadStatus.running => ('Downloading', scheme.primary),
      DownloadStatus.paused => ('Paused', Colors.amber),
      DownloadStatus.failed => ('Failed', scheme.error),
      _ => ('', scheme.outline),
    };

    final size = d.total >= 0 ? '${formatBytes(d.done)} of ${formatBytes(d.total)}' : formatBytes(d.done);
    final details = [
      size,
      if (d.status == DownloadStatus.running && d.speed > 0) formatSpeed(d.speed),
      if (d.eta != null) '${formatDuration(d.eta!)} left',
    ].join(' · ');

    return Card(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(16, 12, 8, 12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Expanded(
                  child: Text(
                    d.displayName,
                    style: Theme.of(context).textTheme.titleMedium,
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
                Chip(
                  label: Text(label),
                  side: BorderSide(color: color),
                  labelStyle: TextStyle(color: color),
                  visualDensity: VisualDensity.compact,
                ),
                const SizedBox(width: 4),
                if (d.status == DownloadStatus.running || d.status == DownloadStatus.queued)
                  IconButton(icon: const Icon(Icons.pause), tooltip: 'Pause', onPressed: onPause),
                if (d.status == DownloadStatus.paused)
                  IconButton(icon: const Icon(Icons.play_arrow), tooltip: 'Resume', onPressed: onResume),
                if (d.status == DownloadStatus.failed)
                  IconButton(icon: const Icon(Icons.refresh), tooltip: 'Try again', onPressed: onResume),
                if (d.status == DownloadStatus.failed)
                  IconButton(icon: const Icon(Icons.delete_outline), tooltip: 'Remove from list', onPressed: onRemove)
                else
                  IconButton(icon: const Icon(Icons.close), tooltip: 'Cancel', onPressed: onCancel),
              ],
            ),
            const SizedBox(height: 8),
            LinearProgressIndicator(
              value: d.status == DownloadStatus.running ? d.fraction : (d.fraction ?? 0),
              color: color,
              minHeight: 6,
              borderRadius: BorderRadius.circular(3),
            ),
            const SizedBox(height: 8),
            Text(details, style: Theme.of(context).textTheme.bodySmall),
            if (d.status == DownloadStatus.failed && d.error != null) ...[
              const SizedBox(height: 4),
              Text(d.error!, style: TextStyle(color: scheme.error, fontSize: 12)),
            ],
          ],
        ),
      ),
    );
  }
}
