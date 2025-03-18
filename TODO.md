# CatchMe TODO List

## Phase 1: Core Functionality ✅
- [x] Basic Downloads
  - [x] Go server starting correctly
  - [x] WebSocket communication
  - [x] Download to ~/Downloads
  - [x] Real-time progress updates

## Phase 2: UI/UX ✅
- [x] Main Interface
  - [x] Design download cards
  - [x] Progress indicators
  - [x] Speed and time display
  - [x] Per-download logs with timestamps
  - [x] Responsive layout for all screen sizes
  - [x] Mobile-first design with drawer navigation

## Phase 3: Extra Features
- [ ] Download Management
  - [ ] Pause/Resume
  - [x] Cancel download
  - [ ] Auto-retry on failures
  - [x] SHA-256 verification

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
- Responsive UI with mobile and tablet support
- Collapsible sidebar for small screens
- Optimized stats layout for different screen sizes
- Consistent spacing and alignment across devices

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
- [ ] Optimize SHA-256 calculation for large files
- [ ] Browser extension
- [ ] Settings
  - [ ] Custom download directory
  - [ ] Speed limits
  - [ ] Concurrent connections
- [ ] Multi-download
- [ ] Themes (GTK/System)
