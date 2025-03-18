# CatchMe TODO List

## Phase 1: Core Functionality ✅
- [x] Basic Downloads
  - [x] Go server starting correctly
  - [x] WebSocket communication
  - [x] Download to ~/Downloads
  - [x] Real-time progress updates
  - [x] Download cancellation
  - [x] Error handling and retry logic

## Phase 2: UI/UX ✅
- [x] Main Interface
  - [x] Design download cards
  - [x] Progress indicators
  - [x] Speed and time display
  - [x] Per-download logs with timestamps
  - [x] Responsive layout for all screen sizes
  - [x] Mobile-first design with drawer navigation
  - [x] Download status indicators
  - [x] Live logs during download

## Phase 3: Extra Features 🚧
- [ ] Download Management
  - [x] Cancel download with proper cleanup
  - [x] Safe download restart
  - [x] Client-server download synchronization
  - [x] Chunked downloading via multiple connections
  - [x] Chunk progress visualization
  - [x] Server-side SHA-256 verification ⭐NEW
  - [x] Auto-retry on failures with exponential backoff
  - [x] Proper pause/resume with chunks ⭐NEW
  - [ ] Download persistence across restarts
  - [ ] Download queue management

## Current Issues Fixed 🛠️
- [x] Fix server connection issues
- [x] Fix type casting errors with speed values
- [x] Fix cancel download functionality
- [x] Fix download restart after cancel
- [x] Fix message handling for canceled downloads
- [x] Fix log display issues
- [x] Improve error handling and messaging
- [x] Prevent app crashes on network errors
- [x] Fix duplicate download item issues
- [x] Improve client-server state synchronization
- [x] Fix chunked downloads visualization
- [x] Move SHA-256 calculation to server-side ⭐NEW
- [x] Optimize server-side checksum calculation ⭐NEW
- [x] Fix pause/resume functionality with chunk state ⭐NEW
- [x] Fix corrupted code in Go server files ⭐NEW
- [x] Improve download performance with increased buffer sizes ⭐NEW

## Current Issues To Fix 🔧
- [ ] Fix visualization of chunks not always appearing consistently
- [ ] Fix issue with downloads stalling at ~85% on some servers
- [ ] Add more robust recovery mechanism for network issues
- [ ] Add download persistence across app restarts
- [ ] Implement proper queuing system

## Achievements & Work in Progress

### ✅ Completed
- Modern Material Design 3 interface with unified colors
- Real-time progress tracking with millisecond precision
- Enhanced download statistics (speed, ETA, average speed)
- Live download logs with formatted timestamps
- Chunked downloads with multiple parallel connections
- Server-side SHA-256 verification (28x faster than client-side)
- Auto-retry mechanism for network issues with exponential backoff
- WebSocket-based communication
- Connection status indicators with improved visibility
- Basic completed downloads view with full checksum display
- Responsive UI with mobile and tablet support
- Collapsible sidebar for small screens
- Optimized stats layout for different screen sizes
- Consistent spacing and alignment across devices
- Better download item management
- Fixed bugs with download cancellation
- Improved log display and organization
- Proper client-server coordination for downloads
- True pause/resume functionality with chunk state preservation ⭐NEW
- Improved download speeds with optimized buffer sizes ⭐NEW
- Better server-side resource usage ⭐NEW
- Better recovery system for interrupted downloads ⭐NEW
- Optimized server-side checksum calculation ⭐NEW

### 🚧 In Progress - Server Improvements
- Further optimization for slow connections
- HTTP range request handling improvements
- More robust cleanup and recovery system
- Persistent download tracking

### 🚧 In Progress - Client Improvements
- More consistent chunk visualization
- Enhanced UI for multi-connection downloads
- Better error feedback and handling
- Robust download queue system
- Download persistence across sessions
- Custom download directory support

## Backlog
- [ ] Browser extension
- [ ] Settings
  - [ ] Custom download directory
  - [ ] Speed limits
  - [ ] Concurrent connections
  - [ ] Default chunk size
- [ ] Multi-file downloads
- [ ] Themes (GTK/System integration)
