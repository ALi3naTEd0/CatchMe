import 'dart:async';
import 'dart:convert';
import 'package:logging/logging.dart';
import 'package:web_socket_channel/web_socket_channel.dart';

enum ConnectionStatus { disconnected, connecting, connected }

class WebSocketConnector {
  static final WebSocketConnector _instance = WebSocketConnector._internal();
  factory WebSocketConnector() => _instance;

  final _logger = Logger('WebSocketConnector');

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
      _logger.info('Connection already in progress, waiting...');
      await _connectionLock.future;
      return;
    }

    _isConnecting = true;
    final connectionCompleter = Completer<void>();
    _connectionLock = connectionCompleter;

    try {
      _statusController.add(ConnectionStatus.connecting);
      _logger.info('Attempting WebSocket connection...');

      // Esperar a que el servidor esté listo
      await Future.delayed(Duration(milliseconds: 500));

      // Intentar conexión con timeout más largo
      for (int i = 0; i < 3; i++) {
        try {
          await _closeExistingChannel();

          _channel = WebSocketChannel.connect(
            Uri.parse('ws://localhost:8080/ws'),
          );
          await _channel!.ready.timeout(
            Duration(seconds: 5),
            onTimeout: () => throw TimeoutException('Connection timeout'),
          );

          _setupListeners();
          _startPingTimer();

          _isConnected = true;
          _reconnectAttempts = 0;
          _statusController.add(ConnectionStatus.connected);
          _logger.info('WebSocket connected successfully');
          return;
        } catch (e) {
          _logger.warning('Connection attempt $i failed: $e');
          await Future.delayed(Duration(seconds: 1));
        }
      }

      throw Exception('Failed to connect after retries');
    } catch (e) {
      _logger.severe('WebSocket connection failed: $e');
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
        _logger.info('Closed existing WebSocket channel');
      } catch (e) {
        _logger.warning('Error closing existing channel: $e');
      }
      _channel = null;
    }
  }

  void send(Map<String, dynamic> message) {
    if (!_isConnected || _channel == null) {
      _logger.warning('Cannot send message: not connected');
      return;
    }

    try {
      final jsonMessage = jsonEncode(message);
      _logger.fine('Sending: $jsonMessage');
      _channel?.sink.add(jsonMessage);
    } catch (e) {
      _logger.severe('WS Error during send: $e');
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
          final data = jsonDecode(message);
          if (data['type'] == 'pong') {
            _logger.fine('Received pong');
          }
        } catch (e) {
          _logger.warning('Error processing message: $e');
        }
      },
      onError: (e) {
        _logger.severe('WS Stream error: $e');
        _handleDisconnect();
      },
      onDone: () {
        _logger.info('WS connection closed');
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
      _logger.info(
        'Reconnecting in ${delay.inMilliseconds}ms (attempt $_reconnectAttempts)',
      );
      _reconnectTimer?.cancel();
      _reconnectTimer = Timer(delay, () {
        connect().catchError((e) {
          _logger.severe('Reconnection attempt failed: $e');
        });
      });
    } else {
      _logger.warning('Max reconnection attempts reached');
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
