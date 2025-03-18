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
    // Definir el color una vez para usarlo en toda la app
    final accentColor = Colors.blue[300] ?? Colors.blue;

    return MaterialApp(
      title: 'CatchMe',
      theme: ThemeData(
        colorScheme: ColorScheme.dark(
          primary: accentColor,      // Color unificado
          secondary: accentColor.withOpacity(0.7),
          surface: const Color(0xFF1E1E1E),
          background: const Color(0xFF121212),
          onPrimary: Colors.white,
        ),
        cardTheme: const CardTheme(
          color: Color(0xFF2A2A2A),
        ),
        navigationRailTheme: NavigationRailThemeData(
          selectedIconTheme: const IconThemeData(
            color: Colors.white,  // Icono blanco cuando está activo
          ),
          selectedLabelTextStyle: TextStyle(
            color: accentColor,   // Usar el mismo accentColor
            fontWeight: FontWeight.bold,
          ),
          unselectedIconTheme: IconThemeData(
            color: Colors.grey[600],  // Icono gris cuando está inactivo
          ),
          unselectedLabelTextStyle: TextStyle(
            color: Colors.grey[600],  // Texto gris cuando está inactivo
          ),
          backgroundColor: Colors.transparent,
          indicatorColor: accentColor,  // Color del indicador seleccionado
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
  String _statusMessage = 'Initializing...';
  
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
    setState(() {
      _statusMessage = 'Starting server...';
    });

    try {
      await ServerLauncher().startServer();
      
      setState(() {
        _statusMessage = 'Connecting to server...';
      });

      // Intentar conectar con timeout
      bool connected = false;
      for (int i = 0; i < 3 && !connected; i++) {
        try {
          await Future.delayed(Duration(seconds: 1));
          setState(() {
            _statusMessage = 'Connecting to server (attempt ${i + 1}/3)...';
          });
          
          // Verificar si el servidor está corriendo
          if (!ServerLauncher().isRunning) {
            throw Exception('Server not running');
          }
          
          connected = true;
        } catch (e) {
          print('Connection attempt $i failed: $e');
        }
      }

      if (!connected) {
        throw Exception('Could not connect to server');
      }

      setState(() {
        _serverStarted = true;
      });
    } catch (e) {
      setState(() {
        _statusMessage = 'Error: $e\nTap to retry';
      });
    }
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
            const SizedBox(width: 8),
            Text(
              isRunning ? 'Server Running' : 'Server Stopped',
              style: TextStyle(
                fontSize: 12,
                fontWeight: FontWeight.bold, // Texto en negrita
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
    final screenWidth = MediaQuery.of(context).size.width;
    final showSidebar = screenWidth > 600; // Hide sidebar on very small screens

    return Scaffold(
      // Drawer para mobile
      drawer: !showSidebar ? _buildNavigationDrawer() : null,
      body: Row(
        children: [
          // Side panel solo si hay espacio
          if (showSidebar)
            Container(
              width: 200,
              child: _buildSidePanel(),
            ),

          if (showSidebar)
            const VerticalDivider(thickness: 1, width: 1),
          
          // Main content
          Expanded(
            child: Column(
              children: [
                if (!showSidebar)
                  AppBar(
                    title: const Text('CatchMe'),
                    backgroundColor: Theme.of(context).colorScheme.surface,
                  ),
                // Show loading if server not started yet
                if (!_serverStarted)
                  Expanded(
                    child: Center(
                      child: Column(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          CircularProgressIndicator(),
                          SizedBox(height: 16),
                          Text(_statusMessage),
                          if (_statusMessage.contains('Error'))
                            TextButton(
                              onPressed: _startServerAndInitServices,
                              child: Text('Retry'),
                            ),
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

  Widget _buildNavigationDrawer() {
    return Drawer(
      child: _buildSidePanel(),
    );
  }

  Widget _buildSidePanel() {
    return Column(
      children: [
        // Navigation rail content
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
        // Extension status
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
                  fontWeight: FontWeight.bold, // Texto en negrita
                  color: Colors.orange,
                ),
              ),
            ],
          ),
        ),
      ],
    );
  }
}