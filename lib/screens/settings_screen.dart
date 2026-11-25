import 'package:flutter/material.dart';

import '../core/catchme_client.dart';

/// What the app is connected to. Download options come in a later step;
/// the old switches here were never wired to anything.
class SettingsScreen extends StatelessWidget {
  const SettingsScreen({super.key, required this.client, required this.appVersion});
  final CatchMeClient client;
  final String appVersion;

  @override
  Widget build(BuildContext context) {
    return ListenableBuilder(
      listenable: client,
      builder:
          (context, _) => ListView(
            padding: const EdgeInsets.all(24),
            children: [
              Text('Settings', style: Theme.of(context).textTheme.headlineSmall),
              const SizedBox(height: 16),
              Card(
                child: Column(
                  children: [
                    ListTile(title: const Text('Download engine'), subtitle: Text(_stateText(client.state))),
                    ListTile(title: const Text('Engine version'), subtitle: Text(client.daemonVersion ?? '—')),
                    ListTile(title: const Text('Address'), subtitle: Text(client.address)),
                  ],
                ),
              ),
              const SizedBox(height: 16),
              Card(
                child: Column(
                  children: [
                    ListTile(title: const Text('App version'), subtitle: Text(appVersion)),
                    const ListTile(
                      title: Text('Source code'),
                      subtitle: SelectableText('https://github.com/ALi3naTEd0/CatchMe'),
                    ),
                  ],
                ),
              ),
            ],
          ),
    );
  }

  static String _stateText(LinkState s) => switch (s) {
    LinkState.connected => 'Connected',
    LinkState.connecting => 'Connecting…',
    LinkState.disconnected => 'Disconnected, retrying',
  };
}
