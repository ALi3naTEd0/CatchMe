import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:logging/logging.dart';

import 'core/catchme_client.dart';
import 'core/daemon.dart';
import 'screens/completed_screen.dart';
import 'screens/downloads_screen.dart';
import 'screens/settings_screen.dart';

/// Set by release builds with --dart-define=CATCHME_VERSION=x.y.z.
const appVersion = String.fromEnvironment('CATCHME_VERSION', defaultValue: '2.0.0-dev');

void main() {
  Logger.root.level = Level.INFO;
  Logger.root.onRecord.listen((r) => debugPrint('${r.level.name} ${r.loggerName}: ${r.message}'));
  runApp(const CatchMeApp());
}

ThemeData _theme() {
  final accent = Colors.blue[300]!;
  return ThemeData(
    useMaterial3: true,
    colorScheme: ColorScheme.dark(
      primary: accent,
      secondary: accent.withAlpha(180),
      surface: const Color(0xFF1E1E1E),
      onPrimary: Colors.black,
    ),
    cardTheme: const CardThemeData(color: Color(0xFF2A2A2A)),
    navigationRailTheme: NavigationRailThemeData(
      backgroundColor: Colors.transparent,
      indicatorColor: accent,
      selectedIconTheme: const IconThemeData(color: Colors.black),
      selectedLabelTextStyle: TextStyle(color: accent, fontWeight: FontWeight.bold),
      unselectedIconTheme: IconThemeData(color: Colors.grey[500]),
      unselectedLabelTextStyle: TextStyle(color: Colors.grey[500]),
    ),
  );
}

class CatchMeApp extends StatelessWidget {
  const CatchMeApp({super.key, this.daemon});

  /// Injected in tests; normally the bundled daemon.
  final Daemon? daemon;

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      debugShowCheckedModeBanner: false,
      title: 'CatchMe',
      theme: _theme(),
      home: StartupScreen(daemon: daemon ?? Daemon()),
    );
  }
}

/// Starts the engine and connects to it, then shows the app.
class StartupScreen extends StatefulWidget {
  const StartupScreen({super.key, required this.daemon});
  final Daemon daemon;

  @override
  State<StartupScreen> createState() => _StartupScreenState();
}

class _StartupScreenState extends State<StartupScreen> {
  CatchMeClient? _client;
  String? _error;

  @override
  void initState() {
    super.initState();
    _start();
  }

  Future<void> _start() async {
    setState(() => _error = null);
    try {
      final addr = await widget.daemon.start();
      final client = CatchMeClient(address: addr, token: widget.daemon.token);
      await client.connect().timeout(const Duration(seconds: 10));
      await _writeSmokeReport(client);
      if (mounted) setState(() => _client = client);
    } catch (e) {
      if (mounted) setState(() => _error = '$e');
    }
  }

  /// CI smoke test: when CATCHME_SMOKE_FILE is set, record that the packaged
  /// app found its bundled engine and connected to it.
  Future<void> _writeSmokeReport(CatchMeClient client) async {
    final path = Platform.environment['CATCHME_SMOKE_FILE'];
    if (path == null || path.isEmpty) return;
    await File(path).writeAsString(
      jsonEncode({
        'connected': client.state == LinkState.connected,
        'engineVersion': client.daemonVersion,
        'engine': Daemon.findBinary(),
        'appVersion': appVersion,
      }),
    );
  }

  @override
  void dispose() {
    _client?.close();
    widget.daemon.stop();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final client = _client;
    if (client != null) return MainScreen(client: client);
    return Scaffold(
      body: Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            if (_error == null) ...[
              const CircularProgressIndicator(),
              const SizedBox(height: 16),
              const Text('Starting the download engine…'),
            ] else ...[
              const Icon(Icons.error_outline, size: 48, color: Colors.redAccent),
              const SizedBox(height: 16),
              Padding(
                padding: const EdgeInsets.symmetric(horizontal: 32),
                child: Text(_error!, textAlign: TextAlign.center),
              ),
              const SizedBox(height: 16),
              FilledButton(onPressed: _start, child: const Text('Try again')),
            ],
          ],
        ),
      ),
    );
  }
}

class MainScreen extends StatefulWidget {
  const MainScreen({super.key, required this.client});
  final CatchMeClient client;

  @override
  State<MainScreen> createState() => _MainScreenState();
}

class _MainScreenState extends State<MainScreen> {
  int _selected = 0;

  @override
  Widget build(BuildContext context) {
    final screens = [
      DownloadsScreen(client: widget.client),
      CompletedScreen(client: widget.client),
      SettingsScreen(client: widget.client, appVersion: appVersion),
    ];
    final wide = MediaQuery.sizeOf(context).width > 600;
    final rail = NavigationRail(
      extended: wide,
      selectedIndex: _selected,
      onDestinationSelected: (i) => setState(() => _selected = i),
      destinations: const [
        NavigationRailDestination(icon: Icon(Icons.download), label: Text('Downloads')),
        NavigationRailDestination(icon: Icon(Icons.check_circle), label: Text('Completed')),
        NavigationRailDestination(icon: Icon(Icons.settings), label: Text('Settings')),
      ],
    );

    return Scaffold(
      body: Row(
        children: [
          SizedBox(width: wide ? 200 : 80, child: rail),
          const VerticalDivider(width: 1),
          Expanded(child: Column(children: [Expanded(child: screens[_selected]), _StatusBar(client: widget.client)])),
        ],
      ),
    );
  }
}

class _StatusBar extends StatelessWidget {
  const _StatusBar({required this.client});
  final CatchMeClient client;

  @override
  Widget build(BuildContext context) {
    return ListenableBuilder(
      listenable: client,
      builder: (context, _) {
        final (text, color) = switch (client.state) {
          LinkState.connected => ('Engine connected', Colors.green),
          LinkState.connecting => ('Connecting…', Colors.amber),
          LinkState.disconnected => ('Engine disconnected, retrying', Colors.redAccent),
        };
        final active = client.downloads.where((d) => d.isActive).length;
        return Container(
          padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 8),
          child: Row(
            children: [
              if (active > 0) Text('$active active', style: const TextStyle(fontSize: 12)),
              const Spacer(),
              Icon(Icons.circle, size: 8, color: color),
              const SizedBox(width: 8),
              Text(text, style: TextStyle(fontSize: 12, color: color, fontWeight: FontWeight.bold)),
            ],
          ),
        );
      },
    );
  }
}
