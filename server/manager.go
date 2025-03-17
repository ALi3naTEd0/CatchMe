package main

import (
    "fmt"
    "net/http"
    "sync"
    "encoding/json"
    "log"

    "github.com/gorilla/websocket"
)

const (
    maxConcurrentChunks = 3
    chunkSize = 5 * 1024 * 1024  // 5MB chunks
    bufferSize = 32 * 1024       // 32KB buffer
)

func (m *Manager) splitIntoChunks(url string, size int64) []*Chunk {
    chunks := make([]*Chunk, 0)
    for i := int64(0); i < size; i += ChunkSize {
        end := i + ChunkSize - 1
        if end > size {
            end = size - 1
        }
        chunks = append(chunks, &Chunk{
            ID:     len(chunks),
            Start:  i,
            End:    end,
            Status: "pending",
        })
    }
    return chunks
}

func (m *Manager) downloadChunk(url string, chunk *Chunk) error {
    req, _ := http.NewRequest("GET", url, nil)
    req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", chunk.Start, chunk.End))
    
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()
    
    // TODO: Implementar escritura a archivo temporal
    chunk.Status = "completed"
    return nil
}

func (m *Manager) sendProgress(url string) {
    m.mu.RLock()
    download := m.downloads[url]
    conn := m.connections[url]
    m.mu.RUnlock()

    if download == nil || conn == nil {
        return
    }

    progress := &Progress{
        URL:           url,
        BytesReceived: download.Downloaded,
        TotalBytes:    download.Size,
        Speed:         0, // TODO: Calcular velocidad
        Status:        download.Status,
    }

    data, _ := json.Marshal(progress)
    conn.WriteMessage(websocket.TextMessage, data)
}

func (m *Manager) sendProgressSafe(url string) {
    m.mu.Lock()
    defer m.mu.Unlock()

    download, ok := m.downloads[url]
    if (!ok) {
        return
    }

    conn, ok := m.connections[url]
    if (!ok) {
        return
    }

    progress := map[string]interface{}{
        "url":           url,
        "bytesReceived": download.Downloaded,
        "totalBytes":    download.Size,
        "status":        download.Status,
        "speed":         download.Speed,
        "error":        download.Error,
    }

    data, _ := json.Marshal(progress)
    conn.WriteMessage(websocket.TextMessage, data)
}

// Añadir nuevo método para iniciar descarga
func (m *Manager) StartDownload(url string, conn *websocket.Conn) {
    m.mu.Lock()
    m.connections[url] = conn
    m.mu.Unlock()

    // Obtener información del archivo
    resp, err := http.Head(url)
    if (err != nil) {
        log.Printf("Error getting file info: %v", err)
        return
    }

    size := resp.ContentLength
    chunks := m.splitIntoChunks(url, size)

    download := &Download{
        URL:      url,
        Size:     size,
        Chunks:   chunks,
        Status:   "downloading",
    }

    m.mu.Lock()
    m.downloads[url] = download
    m.mu.Unlock()

    // Pool de workers para chunks
    semaphore := make(chan struct{}, maxConcurrentChunks)
    
    // Channel para progreso
    progress := make(chan int64, maxConcurrentChunks)

    // Iniciar descarga de chunks
    var wg sync.WaitGroup
    for _, chunk := range chunks {
        wg.Add(1)
        go func(c *Chunk) {
            defer wg.Done()
            semaphore <- struct{}{}
            if err := m.downloadChunk(url, c); err != nil {
                log.Printf("Error downloading chunk %d: %v", c.ID, err)
            }
            m.sendProgress(url)
            <-semaphore
        }(chunk)
    }

    wg.Wait()
    download.Status = "completed"
    m.sendProgress(url)
}
