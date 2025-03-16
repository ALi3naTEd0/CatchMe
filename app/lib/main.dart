import 'package:flutter/material.dart';
import 'downloads_screen.dart';
import 'completed_screen.dart';
import 'settings_screen.dart';

void main() {
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
          primary: const Color(0xFF8B31D3),      // Morado más intenso
          secondary: const Color(0xFFB15EFF),     // Morado secundario
          surface: const Color(0xFF1E1E1E),
          background: const Color(0xFF121212),
          onPrimary: Colors.white,               // Texto blanco sobre morado
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
  Widget build(BuildContext context) {
    return Scaffold(
      body: Row(
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
              // Indicator de conexión simple
              Padding(
                padding: const EdgeInsets.all(8.0),
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
            ],
          ),
          const VerticalDivider(thickness: 1, width: 1),
          Expanded(
            child: _screens[_selectedIndex],
          ),
        ],
      ),
    );
  }
}
