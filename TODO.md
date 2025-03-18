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
  - [ ] Proper pause/resume with state preservation
  - [x] Auto-retry on failures with exponential backoff
  - [x] SHA-256 verification
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

## Current Issues To Fix 🔧
- [ ] Fix visualization of chunks (currently only appears when paused)
- [ ] Fix issue with downloads stalling at ~85%
- [ ] Improve retry mechanism for more reliability
- [ ] Fix chunks not being properly tracked in UI
- [ ] Optimize checksum calculation for large files

## Achievements & Work in Progress

### ✅ Completed
- Modern Material Design 3 interface with unified colors
- Real-time progress tracking with millisecond precision
- Enhanced download statistics (speed, ETA, average speed)
- Live download logs with formatted timestamps
- Chunked downloads for better performance
- Basic SHA-256 verification
- Auto-retry mechanism for network issues with exponential backoff
- WebSocket-based communication
- Connection status indicators
- Basic error handling and recovery
- Responsive UI with mobile and tablet support
- Collapsible sidebar for small screens
- Optimized stats layout for different screen sizes
- Consistent spacing and alignment across devices
- Better download item management
- Fixed bugs with download cancellation
- Improved log display and organization
- Proper client-server coordination for downloads

### 🚧 In Progress - Server Improvements
- True pause/resume functionality with state preservation
- Better recovery system for interrupted downloads
- Better handling of slow connections
- HTTP range and header support optimization
- Optimized checksum calculation (current: ~180s)
- Persistent download tracking

### 🚧 In Progress - Client Improvements
- Always-visible individual chunk progress tracking
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
