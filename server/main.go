package main

import (
    "encoding/json"
    "fmt"
    "io"
    "log"
    "net/http"
    "os"
    "path/filepath"
    "time"
    "github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool { return true },
}

func handleDownload(conn *websocket.Conn, url string) {
    log.Printf("Starting download: %s", url)

    // Get file info
    resp, err := http.Head(url)
    if (err != nil) {
        log.Printf("Error getting file info: %v", err)
        return
    }

    totalSize := resp.ContentLength
    filename := filepath.Base(url)

    // Get home directory
    home, err := os.UserHomeDir()
    if err != nil {
        log.Printf("Error getting home dir: %v", err)
        return
    }

    // Save to Downloads
    savePath := filepath.Join(home, "Downloads", filename)
    resp, err = http.Get(url)
    if err != nil {
        log.Printf("Download error: %v", err)
        return
    }
    defer resp.Body.Close()

    file, err := os.Create(savePath)
    if err != nil {
        log.Printf("Error creating file: %v", err)
        return
    }
    defer file.Close()

    // Download with progress
    buffer := make([]byte, 32*1024)
    var downloaded int64
    startTime := time.Now()

    for {
        n, err := resp.Body.Read(buffer)
        if err != nil && err != io.EOF {
            log.Printf("Read error: %v", err)
            return
        }
        if n == 0 {
            break
        }

        if _, err := file.Write(buffer[:n]); err != nil {
            log.Printf("Write error: %v", err)
            return
        }

        downloaded += int64(n)

        // Send progress more frequently and log it
        progress := map[string]interface{}{
            "type":          "progress",
            "url":           url,
            "bytesReceived": downloaded,
            "totalBytes":    totalSize,
            "status":        "downloading",
            "speed":         float64(downloaded) / time.Since(startTime).Seconds(),
        }
        data, _ := json.Marshal(progress)
        log.Printf("Sending progress: %s", string(data))
        if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
            log.Printf("Error sending progress: %v", err)
        }
    }

    // Send completion
    conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(
        `{"type":"progress","url":"%s","status":"completed"}`, url)))
}

func handleWS(w http.ResponseWriter, r *http.Request) {
    conn, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        log.Printf("Error upgrading connection: %v", err)
        return
    }
    
    log.Printf("New client connected")

    for {
        _, message, err := conn.ReadMessage()
        if err != nil {
            break
        }

        var msg map[string]interface{}
        if err := json.Unmarshal(message, &msg); err != nil {
            continue
        }

        if msg["type"] == "start_download" {
            if url, ok := msg["url"].(string); ok {
                go handleDownload(conn, url)
            }
        }
    }
}

func main() {
    http.HandleFunc("/ws", handleWS)
    log.Printf("Starting server on :8080")
    log.Fatal(http.ListenAndServe(":8080", nil))
}
