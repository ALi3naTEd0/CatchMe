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
    if (_isConnected) return;
    
    if (_isConnecting) {
      print('Connection already in progress, waiting...');
      await _connectionLock.future;
      return;
    }

    _isConnecting = true;
    final connectionCompleter = Completer<void>();
    _connectionLock = connectionCompleter;
    
    try {
      _statusController.add(ConnectionStatus.connecting);
      print('Attempting WebSocket connection...');

      // Esperar a que el servidor esté listo
      await Future.delayed(Duration(milliseconds: 500));

      // Intentar conexión con timeout más largo
      for (int i = 0; i < 3; i++) {
        try {
          await _closeExistingChannel();
          
          _channel = WebSocketChannel.connect(Uri.parse('ws://localhost:8080/ws'));
          await _channel!.ready.timeout(
            Duration(seconds: 5),
            onTimeout: () => throw TimeoutException('Connection timeout'),
          );
          
          _setupListeners();
          _startPingTimer();
          
          _isConnected = true;
          _reconnectAttempts = 0;
          _statusController.add(ConnectionStatus.connected);
          print('WebSocket connected successfully');
          return;
        } catch (e) {
          print('Connection attempt $i failed: $e');
          await Future.delayed(Duration(seconds: 1));
        }
      }
      
      throw Exception('Failed to connect after retries');
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
      // Usar backoff exponencial más agresivo
      final delay = Duration(milliseconds: 500 * _reconnectAttempts);
      print('Reconnecting in ${delay.inMilliseconds}ms (attempt $_reconnectAttempts)');
      _reconnectTimer?.cancel();
      _reconnectTimer = Timer(delay, () {
        connect().catchError((e) {
          print('Reconnection attempt failed: $e');
        });
      });
    } else {
      print('Max reconnection attempts reached');
      // Reset reconnect attempts after a longer delay
      Timer(Duration(seconds: 5), () {
        _reconnectAttempts = 0;
      });
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
