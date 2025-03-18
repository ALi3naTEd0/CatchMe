# CatchMe

A modern, lightweight download manager built with Flutter and Go.

## Features

- 🎨 Modern Material Design 3 interface with consistent color scheme
- 📊 Real-time progress tracking with detailed statistics
- ⏱️ Speed, ETA, and average speed monitoring
- 📝 Live download logs with timestamped entries
- 🔄 Auto-retry on failures with exponential backoff
- 🔒 SHA-256 verification for completed downloads
- 🧠 Smart download management with proper cancellation and restart
- 🧩 Multi-connection chunked downloads for faster speeds
- 🔢 Individual chunk visualization and tracking
- 🌙 Dark theme optimized
- 🎯 Multi-platform support (Linux, Windows, MacOS)

## Technical Features

- **Chunked Downloads**: Download files in multiple parallel connections
- **Real-time Monitoring**: See download speed and progress in real-time
- **WebSocket Communication**: Bidirectional real-time updates between UI and server
- **Smart Retry Logic**: Automatic retry with exponential backoff on network issues
- **Responsive Design**: Works on all screen sizes from mobile to desktop
- **File Integrity**: SHA-256 verification when downloads complete
- **Adaptive UI**: Automatically adjusts to different screen sizes
- **Async Processing**: Non-blocking operations for smooth UI experience
- **Singleton Services**: Efficient state management
- **Cross-Platform**: One codebase for all desktop platforms

## Development

### Prerequisites

- Flutter SDK (3.7.2 or later)
- Go 1.21 or later
- Your favorite IDE (VS Code recommended)

### Getting Started

1. Clone the repository
```bash
git clone https://github.com/your-username/catchme.git
cd catchme
```

2. Install dependencies
```bash
flutter pub get
cd server && go mod download
```

3. Run the development version
```bash
flutter run
```

## Architecture

CatchMe uses a Flutter frontend for the UI and a Go backend for handling downloads. Communication between them is done via WebSocket for real-time updates.

### Client-Server Interaction

- **WebSocket Communication**: Real-time bidirectional communication
- **Download Tracking**: Downloads are tracked on both client and server
- **Synchronized States**: Client and server maintain synchronized download states
- **Cancel Handling**: Special handling for download cancellation and restart
- **Chunk Management**: Server splits downloads into manageable chunks

## Known Issues

- SHA-256 calculation for large files needs optimization
- True pause/resume functionality still being perfected
- Chunks visualization only appears when paused (to be fixed)
- Downloads may stall at ~85% on some servers

## License

[MIT](LICENSE)

## Author

Eduardo Antonio Fortuny Ruvalcaba
