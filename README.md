# CatchMe

A modern, lightweight download manager built with Flutter and Go.

## Features

- 🚀 Clean, modern Material Design 3 interface
- 📊 Real-time progress tracking with detailed statistics
- ⏯️ Pause/Resume functionality
- ✅ SHA-256 verification for completed downloads
- 📝 Live download logs
- 🌙 Dark theme optimized
- 🔄 Auto-retry on failures
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

## Contributing

Pull requests are welcome! For major changes, please open an issue first to discuss what you would like to change.

## License

[MIT](LICENSE)

## Author

Eduardo Antonio Fortuny Ruvalcaba
