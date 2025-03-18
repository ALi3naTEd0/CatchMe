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
- 🌙 Dark theme optimized
- 🎯 Multi-platform support (Linux, Windows, MacOS)

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
./dev.sh
```

## Architecture

CatchMe uses a Flutter frontend for the UI and a Go backend for handling downloads. Communication between them is done via WebSocket for real-time updates.

### Client-Server Interaction

- **WebSocket Communication**: Real-time bidirectional communication
- **Download Tracking**: Downloads are tracked on both client and server
- **Synchronized States**: Client and server maintain synchronized download states
- **Cancel Handling**: Special handling for download cancellation and restart

## Known Issues

- SHA-256 calculation for large files needs optimization
- True pause/resume functionality not yet implemented
- Multi-connection downloads on the roadmap

## License

[MIT](LICENSE)

## Author

Eduardo Antonio Fortuny Ruvalcaba
