package main

import (
    "encoding/json"
    "log"
    "net/http"
    "sync"

    "github.com/gorilla/websocket"
)

// Tipos
type Manager struct {
    downloads   map[string]*Download
    connections map[string]*websocket.Conn
    mu          sync.RWMutex
}

type Download struct {
    URL         string
    Size        int64
    Downloaded  int64
    Status      string
}

// Constructor y métodos del Manager
func NewManager() *Manager {
    return &Manager{
        downloads:   make(map[string]*Download),
        connections: make(map[string]*websocket.Conn),
    }
}

func (m *Manager) StartDownload(url string, conn *websocket.Conn) {
    m.mu.Lock()
    m.connections[url] = conn
    m.mu.Unlock()

    resp, err := http.Head(url)
    if err != nil {
        log.Printf("Error getting file info: %v", err)
        return
    }

    download := &Download{
        URL:    url,
        Size:   resp.ContentLength,
        Status: "downloading",
    }

    m.mu.Lock()
    m.downloads[url] = download
    m.mu.Unlock()

    // Por ahora solo simulamos la descarga
    download.Status = "completed"
    m.sendProgress(url)
}

func (m *Manager) sendProgress(url string) {
    m.mu.RLock()
    download := m.downloads[url]
    conn := m.connections[url]
    m.mu.RUnlock()

    if download == nil || conn == nil {
        return
    }

    progress := map[string]interface{}{
        "url":           url,
        "bytesReceived": download.Downloaded,
        "totalBytes":    download.Size,
        "status":       download.Status,
    }

    data, _ := json.Marshal(progress)
    conn.WriteMessage(websocket.TextMessage, data)
}

// WebSocket setup
var upgrader = websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool {
        return true
    },
}

func handleWS(w http.ResponseWriter, r *http.Request, manager *Manager) {
    conn, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        log.Printf("Error upgrading connection: %v", err)
        return
    }
    defer conn.Close()

    log.Printf("New client connected from %s", conn.RemoteAddr())

    for {
        _, msg, err := conn.ReadMessage()
        if err != nil {
            log.Printf("Error reading message: %v", err)
            break
        }

        var message map[string]interface{}
        if err := json.Unmarshal(msg, &message); err != nil {
            if string(msg) == "ping" {
                conn.WriteMessage(websocket.TextMessage, []byte("pong"))
                continue
            }
            log.Printf("Error parsing message: %v", err)
            continue
        }

        if msgType, ok := message["type"].(string); ok && msgType == "start_download" {
            if url, ok := message["url"].(string); ok {
                go manager.StartDownload(url, conn)
            }
        }
    }
}

func main() {
    manager := NewManager()
    
    http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
        handleWS(w, r, manager)
    })

    port := ":8080"
    log.Printf("Starting server on %s", port)
    if err := http.ListenAndServe(port, nil); err != nil {
        log.Fatal(err)
    }
}
