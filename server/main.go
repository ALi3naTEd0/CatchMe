package main

import (
    "encoding/json"
    "fmt"
    "io"
    "log"
    "net/http"
    "sync"
    "time"

    "github.com/gorilla/websocket"
)

// Tipos
type Manager struct {
    downloads   map[string]*Download
    connections map[*websocket.Conn]bool
    mu          sync.RWMutex
}

type Download struct {
    URL         string
    Size        int64
    Downloaded  int64
    Status      string
    Speed       float64
    StartTime   time.Time
    LastUpdate  time.Time
}

type Message struct {
    Type    string      `json:"type"`
    URL     string      `json:"url,omitempty"`
    Data    interface{} `json:"data,omitempty"`
    Error   string      `json:"error,omitempty"`
}

// Constructor y métodos del Manager
func NewManager() *Manager {
    return &Manager{
        downloads:   make(map[string]*Download),
        connections: make(map[*websocket.Conn]bool),
    }
}

const (
    maxRetries = 3
    retryDelay = 5 * time.Second
)

func (m *Manager) HandleDownload(conn *websocket.Conn, url string) {
    m.mu.Lock()
    download := &Download{
        URL:       url,
        StartTime: time.Now(),
        LastUpdate: time.Now(),
        Status:    "downloading",
    }
    m.downloads[url] = download
    m.mu.Unlock()
    
    log.Printf("Starting download: %s", url)

    // Implementar reintentos a nivel servidor
    var lastError error
    for attempt := 0; attempt <= maxRetries; attempt++ {
        if attempt > 0 {
            m.sendLog(conn, url, fmt.Sprintf("Retry attempt %d/%d", attempt, maxRetries))
            time.Sleep(retryDelay)
        }

        // Get file info
        resp, err := http.Head(url)
        if err != nil {
            lastError = err
            m.sendLog(conn, url, fmt.Sprintf("Error getting file info: %v", err))
            continue
        }
        download.Size = resp.ContentLength
        
        // Log file size
        m.sendLog(conn, url, fmt.Sprintf("File size: %d bytes", download.Size))
        
        // Start download
        req, _ := http.NewRequest("GET", url, nil)
        resp, err = http.DefaultClient.Do(req)
        if err != nil {
            lastError = err
            m.sendLog(conn, url, fmt.Sprintf("Error starting download: %v", err))
            continue
        }
        defer resp.Body.Close()

        // Reset error if we get here
        lastError = nil
        m.sendLog(conn, url, "Download started successfully")
        
        // 3. Read and report progress
        buffer := make([]byte, 32*1024)
        lastReportTime := time.Now()
        
        for {
            // Check if download is paused
            m.mu.RLock()
            isPaused := download.Status == "paused"
            m.mu.RUnlock()
            
            if isPaused {
                time.Sleep(500 * time.Millisecond)
                continue
            }
            
            n, err := resp.Body.Read(buffer)
            if n > 0 {
                m.mu.Lock()
                download.Downloaded += int64(n)
                now := time.Now()
                
                // Update speed more frequently
                if now.Sub(lastReportTime) >= 250*time.Millisecond {
                    elapsed := now.Sub(download.StartTime).Seconds()
                    if elapsed > 0 {
                        download.Speed = float64(download.Downloaded) / elapsed
                    }
                    
                    m.sendProgress(conn, download)
                    lastReportTime = now
                }
                m.mu.Unlock()
            }

            if err == io.EOF {
                break
            }
            if err != nil {
                m.sendError(conn, url, fmt.Sprintf("Error reading: %v", err))
                return
            }
        }

        // Update final status
        m.mu.Lock()
        download.Status = "completed"
        m.mu.Unlock()
        
        m.sendProgress(conn, download)
        log.Printf("Download completed: %s", url)
        break
    }

    if lastError != nil {
        m.sendError(conn, url, fmt.Sprintf("Failed after %d retries: %v", maxRetries, lastError))
        return
    }
}

func (m *Manager) sendProgress(conn *websocket.Conn, download *Download) {
    progress := map[string]interface{}{
        "type":          "progress",
        "url":           download.URL,
        "bytesReceived": download.Downloaded,
        "totalBytes":    download.Size,
        "status":        download.Status,
        "speed":         download.Speed,
    }

    data, _ := json.Marshal(progress)
    conn.WriteMessage(websocket.TextMessage, data)
}

func (m *Manager) sendError(conn *websocket.Conn, url, errorMsg string) {
    log.Printf("Error for %s: %s", url, errorMsg)
    errorData := map[string]interface{}{
        "type":   "error",
        "url":    url,
        "error":  errorMsg,
    }
    data, _ := json.Marshal(errorData)
    conn.WriteMessage(websocket.TextMessage, data)
    
    // Update download status
    m.mu.Lock()
    if download, ok := m.downloads[url]; ok {
        download.Status = "error"
    }
    m.mu.Unlock()
}

func (m *Manager) sendLog(conn *websocket.Conn, url string, message string) {
    logData := map[string]interface{}{
        "type":    "log",
        "url":     url,
        "message": message,
        "time":    time.Now().Format("15:04:05"),
    }
    data, _ := json.Marshal(logData)
    conn.WriteMessage(websocket.TextMessage, data)
}

// Add pause/resume functionality
func (m *Manager) PauseDownload(url string) {
    m.mu.Lock()
    defer m.mu.Unlock()
    
    if download, ok := m.downloads[url]; ok {
        download.Status = "paused"
        
        // Notify about the status change
        for conn := range m.connections {
            m.sendProgress(conn, download)
        }
    }
}

func (m *Manager) ResumeDownload(url string) {
    m.mu.Lock()
    defer m.mu.Unlock()
    
    if download, ok := m.downloads[url]; ok {
        download.Status = "downloading"
        
        // Notify about the status change
        for conn := range m.connections {
            m.sendProgress(conn, download)
        }
    }
}

// WebSocket setup
var upgrader = websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool {
        return true
    },
}

func handleWS(w http.ResponseWriter, r *http.Request) {
    manager := NewManager()
    
    // Upgrade connection
    conn, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        log.Printf("Error upgrading connection: %v", err)
        return
    }
    
    // Register connection
    manager.mu.Lock()
    manager.connections[conn] = true
    manager.mu.Unlock()
    
    log.Printf("New client connected: %s", conn.RemoteAddr())
    
    // Clean up on close
    defer func() {
        conn.Close()
        manager.mu.Lock()
        delete(manager.connections, conn)
        manager.mu.Unlock()
        log.Printf("Client disconnected: %s", conn.RemoteAddr())
    }()

    // Message handling loop
    for {
        _, rawMsg, err := conn.ReadMessage()
        if err != nil {
            if !websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
                log.Printf("WebSocket read error: %v", err)
            }
            break
        }

        // Process message
        var msg Message
        if err := json.Unmarshal(rawMsg, &msg); err != nil {
            log.Printf("Error parsing message: %v", err)
            continue
        }

        // Handle message by type
        switch msg.Type {
        case "start_download":
            if msg.URL != "" {
                go manager.HandleDownload(conn, msg.URL)
            }
        case "pause_download":
            if msg.URL != "" {
                manager.PauseDownload(msg.URL)
            }
        case "resume_download":
            if msg.URL != "" {
                manager.ResumeDownload(msg.URL)
            }
        case "ping":
            conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"pong"}`))
        default:
            log.Printf("Unknown message type: %s", msg.Type)
        }
    }
}

func main() {
    http.HandleFunc("/ws", handleWS)

    // Health check endpoint
    http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
    })

    port := ":8080"
    log.Printf("Starting server on %s", port)
    if err := http.ListenAndServe(port, nil); err != nil {
        log.Fatal(err)
    }
}
