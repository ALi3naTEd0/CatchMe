import 'dart:async';
import 'dart:convert';
import 'package:flutter/foundation.dart';
import 'package:web_socket_channel/web_socket_channel.dart';

enum ConnectionStatus { disconnected, connecting, connected }

class WebSocketConnector {
  static final WebSocketConnector _instance = WebSocketConnector._internal();
  factory WebSocketConnector() => _instance;
  WebSocketConnector._internal();

  final _statusController = StreamController<ConnectionStatus>.broadcast();
  final _messageController = StreamController<String>.broadcast();

  WebSocketChannel? _channel;
  Timer? _reconnectTimer;
  Timer? _pingTimer;
  bool _isConnected = false;
  bool _isConnecting = false;
  int _reconnectAttempts = 0;
  static const _maxReconnectAttempts = 5;
  
  // Cambiar a variable no final para permitir actualización
  Completer<void> _connectionLock = Completer<void>()..complete();
  
  Stream<ConnectionStatus> get statusStream => _statusController.stream;
  Stream<String> get messageStream => _messageController.stream;
  bool get isConnected => _isConnected;

  Future<void> connect() async {
    // Si ya estamos conectados, no hacer nada
    if (_isConnected) return;
    
    // Si ya estamos conectando, esperar a que termine
    if (_isConnecting) {
      print('Connection already in progress, waiting...');
      await _connectionLock.future;
      return;
    }

    // Adquirir lock para conexión
    _isConnecting = true;
    final connectionCompleter = Completer<void>();
    _connectionLock = connectionCompleter;
    
    try {
      _statusController.add(ConnectionStatus.connecting);
      print('Attempting WebSocket connection...');

      // Cerrar cualquier canal existente primero
      await _closeExistingChannel();

      // Crear nuevo canal
      _channel = WebSocketChannel.connect(Uri.parse('ws://localhost:8080/ws'));
      
      // Esperar a que esté listo
      await _channel!.ready.timeout(
        const Duration(seconds: 5),
        onTimeout: () => throw TimeoutException('Connection timed out after 5s'),
      );
      
      // Configurar listeners
      _setupListeners();
      _startPingTimer();
      
      // Actualizar estado
      _isConnected = true;
      _reconnectAttempts = 0;
      _statusController.add(ConnectionStatus.connected);
      print('WebSocket connected successfully');

    } catch (e) {
      print('WebSocket connection failed: $e');
      _handleDisconnect();
    } finally {
      _isConnecting = false;
      connectionCompleter.complete();
    }
  }

  Future<void> _closeExistingChannel() async {
    if (_channel != null) {
      try {
        await _channel!.sink.close();
        print('Closed existing WebSocket channel');
      } catch (e) {
        print('Error closing existing channel: $e');
      }
      _channel = null;
    }
  }

  void send(Map<String, dynamic> message) {
    if (!_isConnected || _channel == null) {
      print('Cannot send message: not connected');
      return;
    }
    
    try {
      final jsonMessage = jsonEncode(message);
      print('Sending: $jsonMessage');
      _channel?.sink.add(jsonMessage);
    } catch (e) {
      print('WS Error during send: $e');
      _handleDisconnect();
    }
  }

  void _setupListeners() {
    // Usar .asBroadcastStream() para permitir múltiples escuchas
    final broadcastStream = _channel!.stream.asBroadcastStream();
    
    broadcastStream.listen(
      (message) {
        try {
          _messageController.add(message as String);
          
          // Manejar pongs específicamente
          final data = jsonDecode(message as String);
          if (data['type'] == 'pong') {
            print('Received pong');
          }
        } catch (e) {
          print('Error processing message: $e');
        }
      },
      onError: (e) {
        print('WS Stream error: $e');
        _handleDisconnect();
      },
      onDone: () {
        print('WS connection closed');
        _handleDisconnect();
      },
    );
  }

  void _handleDisconnect() {
    // Si ya estamos desconectados, no hacer nada
    if (!_isConnected) return;

    _isConnected = false;
    _statusController.add(ConnectionStatus.disconnected);
    _pingTimer?.cancel();
    
    // Limpiar conexión actual
    _closeExistingChannel();

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
    _pingTimer = Timer.periodic(const Duration(seconds: 15), (_) {
      if (_isConnected) {
        send({'type': 'ping'});
      }
    });
  }

  void dispose() {
    _reconnectTimer?.cancel();
    _pingTimer?.cancel();
    _closeExistingChannel();
    _statusController.close();
    _messageController.close();
  }
}
