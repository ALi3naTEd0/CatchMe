import 'package:flutter/material.dart';
import 'downloads_screen.dart';
import 'completed_screen.dart';
import 'settings_screen.dart';
import 'server_launcher.dart';  // Update import path

void main() async {
  WidgetsFlutterBinding.ensureInitialized();
  
  print('\n=== CatchMe Iniciando ===');
  runApp(const MyApp());
}

class MyApp extends StatelessWidget {
  const MyApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'CatchMe',
      theme: ThemeData(
        colorScheme: ColorScheme.dark(
          primary: const Color(0xFF2196F3),      // Azul material
          secondary: const Color(0xFF64B5F6),     // Azul más claro
          surface: const Color(0xFF1E1E1E),
          background: const Color(0xFF121212),
          onPrimary: Colors.white,               // Texto blanco sobre azul
        ),
        cardTheme: const CardTheme(
          color: Color(0xFF2A2A2A),              // Fondo de cards más oscuro
        ),
        useMaterial3: true,
      ),
      home: const MainScreen(),
    );
  }
}

class MainScreen extends StatefulWidget {
  const MainScreen({super.key});

  @override
  State<MainScreen> createState() => _MainScreenState();
}

class _MainScreenState extends State<MainScreen> {
  int _selectedIndex = 0;
  
  static const List<Widget> _screens = [
    DownloadsScreen(),
    CompletedScreen(),
    SettingsScreen(),
  ];

  @override
  void initState() {
    super.initState();
    print('\n=== Iniciando servidor Go ===');
    // Iniciar servidor después de que la UI esté lista
    WidgetsBinding.instance.addPostFrameCallback((_) async {
      print('Llamando a ServerLauncher.startServer()');
      await ServerLauncher().startServer();
      print('ServerLauncher.startServer() completado');
    });
  }

  @override
  void dispose() {
    // Asegurarse de cerrar el servidor al salir
    ServerLauncher().stopServer();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: Stack(
        children: [
          Row(
            children: [
              Column(
                children: [
                  Expanded(
                    child: NavigationRail(
                      extended: true,
                      destinations: const [
                        NavigationRailDestination(
                          icon: Icon(Icons.download),
                          label: Text('Downloads'),
                        ),
                        NavigationRailDestination(
                          icon: Icon(Icons.check_circle),
                          label: Text('Completed'),
                        ),
                        NavigationRailDestination(
                          icon: Icon(Icons.settings),
                          label: Text('Settings'),
                        ),
                      ],
                      selectedIndex: _selectedIndex,
                      onDestinationSelected: (int index) {
                        setState(() {
                          _selectedIndex = index;
                        });
                      },
                    ),
                  ),
                ],
              ),
              const VerticalDivider(thickness: 1, width: 1),
              Expanded(child: _screens[_selectedIndex]),
            ],
          ),
          // Status indicators at bottom
          Positioned(
            left: 0,
            right: 0,
            bottom: 16,
            child: Row(
              children: [
                // Browser connection status
                Padding(
                  padding: const EdgeInsets.only(left: 16),
                  child: Container(
                    padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
                    decoration: BoxDecoration(
                      color: Theme.of(context).colorScheme.surface,
                      borderRadius: BorderRadius.circular(16),
                    ),
                    child: Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        Icon(
                          Icons.cloud_off,
                          size: 16,
                          color: Colors.orange,
                        ),
                        const SizedBox(width: 4),
                        Text(
                          'Disconnected',
                          style: TextStyle(
                            fontSize: 12,
                            color: Colors.orange,
                          ),
                        ),
                      ],
                    ),
                  ),
                ),
                const Spacer(),
                // Server status
                Padding(
                  padding: const EdgeInsets.only(right: 16),
                  child: StreamBuilder<bool>(
                    stream: ServerLauncher().statusStream,
                    initialData: false,
                    builder: (context, snapshot) {
                      final isRunning = snapshot.data ?? false;
                      return Container(
                        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
                        decoration: BoxDecoration(
                          color: Theme.of(context).colorScheme.surface,
                          borderRadius: BorderRadius.circular(16),
                        ),
                        child: Row(
                          mainAxisSize: MainAxisSize.min,
                          children: [
                            Icon(
                              isRunning ? Icons.dns : Icons.dns_outlined,
                              size: 16,
                              color: isRunning ? Colors.green : Colors.red,
                            ),
                            const SizedBox(width: 4),
                            Text(
                              isRunning ? 'Server Running' : 'Server Stopped',
                              style: TextStyle(
                                fontSize: 12,
                                color: isRunning ? Colors.green : Colors.red,
                              ),
                            ),
                          ],
                        ),
                      );
                    },
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}
