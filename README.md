# CatchMe

A modern download manager with browser integration.

## Features
- Multi-threaded downloads with chunk support
- Real-time progress tracking
- Download pause/resume
- Browser integration (coming soon)
- Dark theme UI

## Project Structure
- `app/` - Flutter application with Material 3 design
- `server/` - Go-based download manager service
- `extensions/` - Browser extensions (coming soon)

## Development

### Prerequisites
- Flutter 3.x
- Go 1.21+
- CMake (for Linux build)

### Quick Start
```bash
# Start both server and UI
./dev.sh
```

### Manual Setup
1. Start the server:
```bash
cd server
go run main.go
```

2. Start the Flutter app:
```bash
cd app
flutter run -d linux  # or windows/macos
```
