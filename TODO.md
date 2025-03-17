# CatchMe TODO List

## High Priority (Current Sprint)
- [ ] Fix basic functionality
  - [ ] Server starts correctly
  - [ ] WebSocket connection works
  - [ ] Downloads go to correct folder
  - [ ] Progress updates shown in UI

- [ ] Download paths and storage
  - [ ] Use system Downloads folder (/home/x/Downloads/catchme)
  - [ ] Create folder if not exists
  - [ ] Verify write permissions
  - [ ] Show correct path in logs

- [ ] UI Improvements
  - [ ] Enhanced download card layout
  - [ ] Real-time progress indicators
  - [ ] Connection status indicators
  - [ ] Download logs integration

## Next Steps
- [ ] Improve download completion
  - [ ] Show downloads in "Completed" screen after finishing
  - [x] SHA-256 verification on completion
  - [x] Show folder icon instead of play button when completed
  - [ ] Add source verification option

- [ ] Enhance download monitoring
  - [x] Add detailed mini-log below each download
  - [x] Clear speed/time indicators ("Speed:", "Avg:", "ETA:", "Time:")
  - [ ] Auto-detect URLs from clipboard
  - [ ] Add copy-paste area for URLs

## Settings Implementation
- [ ] Download Configuration
  - [ ] Custom download location picker
  - [ ] Default formats selection
  - [ ] Browser download interception options
  - [ ] Custom format lists

- [ ] Application Settings
  - [ ] Theme selector
  - [ ] Browser extension management
  - [ ] Auto-update preferences
  - [ ] Language selection

## Features Backlog
- [ ] Browser Extensions
  - [ ] Chrome extension stub
  - [ ] Firefox extension stub
  - [ ] Extension installation guides
  - [ ] Extension store references

- [ ] Download Management
  - [ ] Multiple simultaneous downloads
  - [ ] Download queue system
  - [ ] Download prioritization
  - [ ] Bandwidth control

## Infrastructure
- [ ] About Section
  - [ ] App info & version
  - [ ] Author credit (Eduardo Antonio Fortuny Ruvalcaba)
  - [ ] MIT license
  - [ ] Repository link
  - [ ] Update checker

## Documentation
- [ ] API documentation
- [ ] User guide
- [ ] Extension development guide
- [ ] Contribution guidelines

## Testing
- [ ] Unit tests for core functionality
- [ ] Integration tests
- [ ] UI tests
- [ ] Cross-platform testing

## Distribution
- [ ] Release workflow
- [ ] Package for different platforms
- [ ] Auto-update mechanism
- [ ] Extension store publications
