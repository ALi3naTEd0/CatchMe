# Changelog

## [Unreleased]

### Added
- Modern Material Design 3 interface with dark theme
- Real-time download progress tracking
- Enhanced download statistics (speed, ETA, average speed)
- Live download logs with formatted timestamps
- SHA-256 file verification
- Pause/Resume functionality
- Auto-retry mechanism with exponential backoff
- Multi-connection chunked downloads
- WebSocket-based communication between UI and server
- Connection status indicators with improved visibility
- Basic completed downloads view with full checksum display
- Download tracking synchronization between client and server
- Chunk visualization during downloads
- Improved error recovery logic
- More robust server restart handling

### Enhanced
- Responsive layout support for all screen sizes
- Mobile-friendly interface with collapsible navigation
- Optimized stats display for smaller screens
- Better alignment and spacing in mobile view
- Automatic layout adaptation based on screen width
- Drawer navigation for mobile devices
- Improved download cancellation handling
- Fixed download restart issues after cancellation
- Better client-server state coordination
- More reliable connection management
- Improved server process handling
- Faster download speeds with multi-connection downloads
- More detailed error and status reporting
- Better progress tracking for large files

### Fixed
- Server-client synchronization for canceled downloads
- Message handling for canceled downloads
- Download restart after cancellation
- Type casting errors with speed values
- Repeated server messages after cancellation
- UI glitches when restarting downloads
- Download loss on connection errors
- Download state inconsistencies
- Memory leaks during long downloads
- Unstable connections with large files
- Unexpected download termination
- Retry mechanism failures
- Connection handling after network interruptions

### Technical
- Implemented singleton pattern for services
- Real-time progress updates via WebSocket
- Proper error handling and retry logic with backoff
- Development environment setup
- Server process management
- Unified color scheme and typography
- Enhanced logging system with timestamps
- Responsive breakpoints implementation
- Mobile-first design approach
- Adaptive UI components
- Cancel handling with recently canceled list
- Active downloads server-side tracking
- Chunked download implementation
- Range request support for partial downloads
- Multi-connection concurrent downloads
- Reliable server cleanup on exit

### Infrastructure
- Project documentation updated
- Development script for easy startup
- Build configurations for multi-platform support
- Server auto-recovery
- Robust error handling
