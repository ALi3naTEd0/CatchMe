# CatchMe TODO List

## Fase 1: Funcionalidad Base ✅
- [x] Descarga básica
  - [x] Servidor Go iniciando correctamente
  - [x] WebSocket funcionando
  - [x] Descarga a ~/Downloads
  - [x] Progreso actualizado en tiempo real

## Fase 2: UI/UX ✅
- [x] Interfaz principal
  - [x] Diseño de tarjetas de descarga
  - [x] Indicadores de progreso
  - [x] Mostrar velocidad y tiempo
  - [x] Logs por descarga con timestamps

## Fase 3: Características Extra ✅
- [ ] Gestión de descargas
  - [ ] Pausar/Reanudar
  - [x] Cancelar descarga
  - [ ] Auto-retry en fallos
  - [x] Verificación SHA-256

## Achievements & Work in Progress

### ✅ Completed
- Modern Material Design 3 interface with unified colors
- Real-time progress tracking with millisecond precision
- Enhanced download statistics (speed, ETA, average speed)
- Live download logs with formatted timestamps
- Basic SHA-256 verification
- Auto-retry mechanism for network issues
- WebSocket-based communication
- Connection status indicators
- Basic error handling and recovery

### 🚧 In Progress - Server Improvements
- Chunked downloads support
- True pause/resume functionality
- Multi-connection downloads
- Recovery system for interrupted downloads
- Better handling of slow connections
- HTTP range and header support
- Optimized checksum calculation (current: ~180s)

### 🚧 In Progress - Client Improvements
- Individual chunk progress tracking
- Enhanced UI for multi-connection downloads
- Better error feedback and handling
- Robust download queue system
- Download persistence across sessions
- Custom download directory support
- Browser extension integration

## Backlog
- [ ] Optimizar cálculo de SHA-256 para archivos grandes
- [ ] Extensión navegador
- [ ] Configuraciones
  - [ ] Carpeta de descarga personalizable
  - [ ] Límite de velocidad
  - [ ] Conexiones simultáneas
- [ ] Multi-descarga
- [ ] Temas (GTK/System)
