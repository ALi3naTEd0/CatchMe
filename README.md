# CatchMe

A modern, lightweight download manager built with Flutter and Go.

## Features

- 🎨 Modern Material Design 3 interface with consistent color scheme
- 📊 Real-time progress tracking with detailed statistics
- ⏱️ Speed, ETA, and average speed monitoring
- 📝 Live download logs with timestamped entries
- ⏯️ Pause/Resume functionality
- ✅ SHA-256 verification for completed downloads
- 🔄 Auto-retry on failures with exponential backoff
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

## Known Issues

- SHA-256 calculation for large files needs optimization
- Server connection status could be more robust

## License

[MIT](LICENSE)

## Author

Eduardo Antonio Fortuny Ruvalcaba
