import 'dart:async';
import 'dart:convert';
import 'package:flutter/foundation.dart';  // Agregar esta línea
import 'package:web_socket_channel/web_socket_channel.dart';

enum ConnectionStatus { disconnected, connecting, connected }

class WebSocketConnector {
  static final WebSocketConnector _instance = WebSocketConnector._internal();
  factory WebSocketConnector() => _instance;
  WebSocketConnector._internal();

  final _statusController = StreamController<ConnectionStatus>.broadcast();
  final _messageController = StreamController<String>.broadcast();  // Cambiar a String

  WebSocketChannel? _channel;
  Timer? _reconnectTimer;
  Timer? _pingTimer;
  bool _isConnected = false;
  int _reconnectAttempts = 0;
  static const _maxReconnectAttempts = 5;
  
  Stream<ConnectionStatus> get statusStream => _statusController.stream;
  Stream<String> get messageStream => _messageController.stream;  // Cambiar a String
  bool get isConnected => _isConnected;

  Future<void> connect() async {
    if (_isConnected) return;

    try {
      _statusController.add(ConnectionStatus.connecting);
      print('Connecting to WebSocket server...');

      _channel = WebSocketChannel.connect(Uri.parse('ws://localhost:8080/ws'));
      await _channel!.ready;
      
      _setupListeners();
      _startPingTimer();
      
      _isConnected = true;
      _reconnectAttempts = 0;
      _statusController.add(ConnectionStatus.connected);
      print('Connected to WebSocket server');

    } catch (e) {
      print('WebSocket connection failed: $e');
      _handleDisconnect();
    }
  }

  void send(Map<String, dynamic> message) {
    if (!_isConnected) return;
    
    try {
      _channel?.sink.add(jsonEncode(message));
    } catch (e) {
      // Solo log crítico
      if (kDebugMode) print('WS Error: $e');
      _handleDisconnect();
    }
  }

  void _setupListeners() {
    _channel?.stream.listen(
      (message) => _messageController.add(message as String),
      onError: (e) {
        if (kDebugMode) print('WS Error: $e');
        _handleDisconnect();
      },
      onDone: () => _handleDisconnect(),
    );
  }

  void _handleDisconnect() {
    _isConnected = false;
    _statusController.add(ConnectionStatus.disconnected);
    _pingTimer?.cancel();

    if (_reconnectAttempts < _maxReconnectAttempts) {
      _reconnectAttempts++;
      final delay = Duration(seconds: _reconnectAttempts * 2); // Backoff exponencial
      print('Reconnecting in ${delay.inSeconds} seconds (attempt $_reconnectAttempts)');
      _reconnectTimer?.cancel();
      _reconnectTimer = Timer(delay, connect);
    } else {
      print('Max reconnection attempts reached');
    }
  }

  void _startPingTimer() {
    _pingTimer?.cancel();
    _pingTimer = Timer.periodic(Duration(seconds: 30), (_) {
      if (_isConnected) {
        send({'type': 'ping'});
      }
    });
  }

  void dispose() {
    _reconnectTimer?.cancel();
    _pingTimer?.cancel();
    _channel?.sink.close();
    _statusController.close();
    _messageController.close();
  }
}
