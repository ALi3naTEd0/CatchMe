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
          primary: Colors.blue,      // Cambiar a azul
          secondary: Colors.blue.withOpacity(0.7),
          surface: const Color(0xFF1E1E1E),
          background: const Color(0xFF121212),
          onPrimary: Colors.white,
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
  bool _serverStarted = false;
  
  static const List<Widget> _screens = [
    DownloadsScreen(),
    CompletedScreen(),
    SettingsScreen(),
  ];

  @override
  void initState() {
    super.initState();
    _startServerAndInitServices();
  }
  
  // Método para iniciar servidor y después servicios
  Future<void> _startServerAndInitServices() async {
    print('\n=== Iniciando servidor Go ===');
    await ServerLauncher().startServer();
    print('ServerLauncher.startServer() completado');
    
    // Pequeño delay para asegurar que el servidor esté listo
    await Future.delayed(const Duration(milliseconds: 500));
    setState(() {
      _serverStarted = true;
    });
  }

  @override
  void dispose() {
    // Asegurarse de cerrar el servidor al salir
    ServerLauncher().stopServer();
    super.dispose();
  }

  Widget _buildServerStatus() {
    return StreamBuilder<bool>(
      stream: ServerLauncher().statusStream,
      initialData: false,
      builder: (context, snapshot) {
        final isRunning = snapshot.data ?? false;
        return Row(
          children: [
            Container(
              width: 8,
              height: 8,
              decoration: BoxDecoration(
                shape: BoxShape.circle,
                color: isRunning ? Colors.green : Colors.red,
              ),
            ),
            SizedBox(width: 8),
            Text(
              isRunning ? 'Server Running' : 'Server Stopped',
              style: TextStyle(
                fontSize: 12,
                color: isRunning ? Colors.green : Colors.red,
              ),
            ),
          ],
        );
      },
    );
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: Row(
        children: [
          // Side panel
          Container(
            width: 200,
            child: Column(
              children: [
                // Navigation rail
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
                
                // Estado de extensión (abajo izquierda)
                Container(
                  padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
                  margin: const EdgeInsets.only(bottom: 8, left: 8, right: 8),
                  decoration: BoxDecoration(
                    color: Theme.of(context).colorScheme.surface,
                    borderRadius: BorderRadius.circular(4),
                  ),
                  child: Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Icon(
                        Icons.extension,
                        size: 16,
                        color: Colors.orange,
                      ),
                      SizedBox(width: 8),
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
              ],
            ),
          ),

          const VerticalDivider(thickness: 1, width: 1),
          
          // Main content area
          Expanded(
            child: Column(
              children: [
                // Show loading if server not started yet
                if (!_serverStarted)
                  const Expanded(
                    child: Center(
                      child: Column(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          CircularProgressIndicator(),
                          SizedBox(height: 16),
                          Text('Starting server...'),
                        ],
                      ),
                    ),
                  )
                else
                  // Screen content
                  Expanded(child: _screens[_selectedIndex]),
                
                // Server status bar
                Container(
                  padding: const EdgeInsets.symmetric(horizontal: 32, vertical: 8),
                  margin: const EdgeInsets.only(bottom: 8),
                  color: Theme.of(context).colorScheme.surface,
                  child: Row(
                    mainAxisAlignment: MainAxisAlignment.end,
                    children: [
                      _buildServerStatus(),
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
}