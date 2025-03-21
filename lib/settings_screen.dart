import 'package:flutter/material.dart';
import 'package:shared_preferences/shared_preferences.dart';

class SettingsScreen extends StatefulWidget {
  const SettingsScreen({super.key});

  @override
  State<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends State<SettingsScreen> {
  // Opciones de configuración
  bool _useChunkedDownloads = true;
  bool _includeChunkDetailsInLog = false;
  bool _useBackgroundService = false;
  
  @override
  void initState() {
    super.initState();
    _loadSettings();
  }
  
  Future<void> _loadSettings() async {
    final prefs = await SharedPreferences.getInstance();
    setState(() {
      _useChunkedDownloads = prefs.getBool('use_chunked_downloads') ?? true;
      _includeChunkDetailsInLog = prefs.getBool('include_chunk_details') ?? false;
      _useBackgroundService = prefs.getBool('use_background_service') ?? false;
    });
  }
  
  Future<void> _saveSettings() async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setBool('use_chunked_downloads', _useChunkedDownloads);
    await prefs.setBool('include_chunk_details', _includeChunkDetailsInLog);
    await prefs.setBool('use_background_service', _useBackgroundService);
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Settings'),
        backgroundColor: Theme.of(context).colorScheme.surface,
      ),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          Card(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Padding(
                  padding: const EdgeInsets.fromLTRB(16, 16, 16, 8),
                  child: Text(
                    'Download Settings',
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                ),
                SwitchListTile(
                  title: const Text('Use chunked downloads'),
                  subtitle: const Text('Download files using multiple parallel connections'),
                  value: _useChunkedDownloads,
                  onChanged: (bool value) {
                    setState(() {
                      _useChunkedDownloads = value;
                    });
                    _saveSettings();
                  },
                ),
                SwitchListTile(
                  title: const Text('Show chunk details in log'),
                  subtitle: const Text('Include detailed progress of each chunk in download log'),
                  value: _includeChunkDetailsInLog,
                  onChanged: (bool value) {
                    setState(() {
                      _includeChunkDetailsInLog = value;
                    });
                    _saveSettings();
                  },
                ),
                const Divider(),
                SwitchListTile(
                  title: const Text('Run as background service'),
                  subtitle: const Text('Continue downloads even when app is closed'),
                  value: _useBackgroundService,
                  onChanged: (bool value) {
                    setState(() {
                      _useBackgroundService = value;
                    });
                    _saveSettings();
                  },
                ),
              ],
            ),
          ),
          const SizedBox(height: 16),
          Card(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Padding(
                  padding: const EdgeInsets.fromLTRB(16, 16, 16, 8),
                  child: Text(
                    'About',
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                ),
                ListTile(
                  title: const Text('Version'),
                  subtitle: const Text('1.0.0'),
                  trailing: const Icon(Icons.info_outline),
                ),
                const Divider(),
                ListTile(
                  title: const Text('Source Code'),
                  subtitle: const Text('View on GitHub'),
                  trailing: const Icon(Icons.code),
                  onTap: () {
                    // Abrir enlace a GitHub
                  },
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}
