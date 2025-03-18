package main

import (
    "encoding/json"
    "fmt"
    "io"
    "log"
    "net/http"
    "os"
    "path/filepath"
    "sync"
    "time"
    "github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool { return true },
}

// Mutex para sincronizar escrituras al websocket
type SafeConn struct {
    conn *websocket.Conn
    mu   sync.Mutex
}

// SendJSON envía un mensaje JSON de forma segura
func (sc *SafeConn) SendJSON(v interface{}) error {
    sc.mu.Lock()
    defer sc.mu.Unlock()
    return sc.conn.WriteJSON(v)
}

// SendText envía un mensaje de texto de forma segura
func (sc *SafeConn) SendText(message string) error {
    sc.mu.Lock()
    defer sc.mu.Unlock()
    return sc.conn.WriteMessage(websocket.TextMessage, []byte(message))
}

func handleDownload(safeConn *SafeConn, url string) {
    log.Printf("Starting download: %s", url)

    // Enviar confirmación inicial al cliente
    sendMessage(safeConn, "log", url, "Starting download process...")

    // Simple client without timeout that could break the connection
    client := &http.Client{}

    // Obtener información del archivo con manejo de errores mejorado
    resp, err := client.Head(url)
    if err != nil {
        log.Printf("Error getting file info for %s: %v", url, err)
        sendMessage(safeConn, "error", url, fmt.Sprintf("Error getting file info: %v", err))
        return
    }

    totalSize := resp.ContentLength
    filename := filepath.Base(url)
    
    sendMessage(safeConn, "log", url, fmt.Sprintf("File size: %d bytes", totalSize))

    // Asegurar que el directorio de descargas existe
    home, err := os.UserHomeDir()
    if err != nil {
        log.Printf("Error getting home directory: %v", err)
        sendMessage(safeConn, "error", url, "Could not determine download location")
        return
    }
    
    downloadDir := filepath.Join(home, "Downloads")
    savePath := filepath.Join(downloadDir, filename)
    
    // Iniciar la descarga real
    sendMessage(safeConn, "log", url, "Starting download...")
    resp, err = client.Get(url)
    if err != nil {
        log.Printf("Download error for %s: %v", url, err)
        sendMessage(safeConn, "error", url, fmt.Sprintf("Download error: %v", err))
        return
    }
    defer resp.Body.Close()

    // Crear archivo de destino
    file, err := os.Create(savePath)
    if err != nil {
        log.Printf("Error creating file %s: %v", savePath, err)
        sendMessage(safeConn, "error", url, fmt.Sprintf("Error creating file: %v", err))
        return
    }
    defer file.Close()

    // Variables para seguimiento del progreso
    buffer := make([]byte, 32*1024)
    var downloaded int64
    startTime := time.Now()
    lastUpdate := time.Now()
    
    // Enviar estado inicial
    sendProgress(safeConn, url, downloaded, totalSize, 0)

    // Loop de descarga con mejor manejo de errores
    for {
        n, err := resp.Body.Read(buffer)
        if n > 0 {
            // Escribir al archivo
            _, writeErr := file.Write(buffer[:n])
            if writeErr != nil {
                log.Printf("Error writing to file %s: %v", savePath, writeErr)
                sendMessage(safeConn, "error", url, fmt.Sprintf("Write error: %v", writeErr))
                return
            }
            
            downloaded += int64(n)
        }
        
        // Actualizar progreso cada 200ms
        if time.Since(lastUpdate) >= 200*time.Millisecond {
            elapsedSeconds := time.Since(startTime).Seconds()
            speed := float64(downloaded) / elapsedSeconds
            
            sendProgress(safeConn, url, downloaded, totalSize, speed)
            lastUpdate = time.Now()
        }
        
        // Manejar fin o error de lectura
        if err != nil {
            if err == io.EOF {
                // Descarga completa
                break
            }
            
            log.Printf("Error reading from response for %s: %v", url, err)
            sendMessage(safeConn, "error", url, fmt.Sprintf("Read error: %v", err))
            return
        }
    }

    // Verificar si la descarga está completa
    if totalSize > 0 && downloaded != totalSize {
        log.Printf("Download incomplete for %s: got %d bytes, expected %d", url, downloaded, totalSize)
        sendMessage(safeConn, "error", url, fmt.Sprintf("Download incomplete: got %d bytes, expected %d", downloaded, totalSize))
        return
    }

    // Enviar confirmación de completado
    log.Printf("Download completed: %s (%d bytes)", filename, downloaded)
    sendMessage(safeConn, "log", url, fmt.Sprintf("Download completed: %d bytes", downloaded))
    sendProgress(safeConn, url, downloaded, totalSize, 0, "completed")
}

// Función mejorada para enviar mensajes
func sendMessage(safeConn *SafeConn, msgType, url, message string) {
    data := map[string]interface{}{
        "type": msgType,
        "url": url,
        "message": message,
    }
    
    if err := safeConn.SendJSON(data); err != nil {
        log.Printf("Error sending message to client: %v", err)
    }
}

// Función mejorada para enviar progreso
func sendProgress(safeConn *SafeConn, url string, bytesReceived, totalBytes int64, speed float64, status ...string) {
    downloadStatus := "downloading"
    if len(status) > 0 {
        downloadStatus = status[0]
    }
    
    data := map[string]interface{}{
        "type": "progress",
        "url": url,
        "bytesReceived": bytesReceived,
        "totalBytes": totalBytes,
        "speed": speed,
        "status": downloadStatus,
    }
    
    if err := safeConn.SendJSON(data); err != nil {
        log.Printf("Error sending progress to client: %v", err)
    }
}

func handleWS(w http.ResponseWriter, r *http.Request) {
    // Mejorar el log con información de cliente
    log.Printf("WebSocket connection request from %s", r.RemoteAddr)
    
    conn, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        log.Printf("Error upgrading connection: %v", err)
        return
    }
    
    // Crear conexión segura con mutex
    safeConn := &SafeConn{conn: conn}
    
    // Configuración sin timeouts para evitar desconexiones
    conn.SetReadDeadline(time.Time{})
    
    log.Printf("Client connected: %s", r.RemoteAddr)
    
    // Cleanup al finalizar
    defer func() {
        conn.Close()
        log.Printf("Client disconnected: %s", r.RemoteAddr)
    }()
    
    // Manejar mensajes
    for {
        _, message, err := conn.ReadMessage()
        if err != nil {
            // Log más descriptivo sobre desconexiones
            if websocket.IsUnexpectedCloseError(err) {
                log.Printf("Client %s disconnected: %v", r.RemoteAddr, err)
            } else {
                log.Printf("WebSocket error from %s: %v", r.RemoteAddr, err)
            }
            break
        }
        
        // Decodificar el mensaje
        var msg map[string]interface{}
        if err := json.Unmarshal(message, &msg); err != nil {
            log.Printf("Invalid message format: %v", err)
            continue
        }
        
        // Manejar tipos de mensajes
        switch msg["type"] {
        case "start_download":
            if url, ok := msg["url"].(string); ok {
                log.Printf("Download request for: %s", url)
                go handleDownload(safeConn, url)
            } else {
                log.Printf("Invalid download request, missing URL")
            }
        case "ping":
            // Responder a pings para mantener la conexión viva
            safeConn.SendJSON(map[string]string{"type": "pong"})
        default:
            log.Printf("Unhandled message type: %v", msg["type"])
        }
    }
}

func main() {
    // Asegurarse de que existe el directorio de logs
    err := os.MkdirAll("logs", os.ModePerm)
    if err != nil {
        log.Printf("Failed to create logs directory: %v", err)
    }
    
    // Configurar logging a archivo
    logFile, err := os.OpenFile("logs/server.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
    if err != nil {
        log.Printf("Failed to open log file: %v", err)
    } else {
        log.SetOutput(io.MultiWriter(os.Stdout, logFile))
    }
    
    http.HandleFunc("/ws", handleWS)
    log.Printf("Starting server on :8080")
    log.Fatal(http.ListenAndServe(":8080", nil))
}
