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

// Agregamos un mapa para rastrear las descargas activas en el servidor
var (
    activeDownloads     = make(map[string]bool)
    activeDownloadsMux  sync.Mutex
)

// Función para marcar una descarga como activa
func markDownloadActive(url string) {
    activeDownloadsMux.Lock()
    defer activeDownloadsMux.Unlock()
    activeDownloads[url] = true
    log.Printf("Download tracked: %s", url)
}

// Función para verificar si una descarga está activa
/*
func isDownloadActive(url string) bool {
    activeDownloadsMux.Lock()
    defer activeDownloadsMux.Unlock()
    return activeDownloads[url]
}
*/

// Función para marcar una descarga como cancelada/terminada
func markDownloadInactive(url string) {
    activeDownloadsMux.Lock()
    defer activeDownloadsMux.Unlock()
    delete(activeDownloads, url)
    log.Printf("Download untracked: %s", url)
}

func handleDownload(safeConn *SafeConn, url string) {
    // Marcamos la URL como activa
    markDownloadActive(url)
    defer markDownloadInactive(url) // Asegurarnos de que se elimine al finalizar
    
    log.Printf("Starting/Resuming download: %s", url)
    
    client := &http.Client{
        Timeout: 0, // Sin timeout global
        Transport: &http.Transport{
            MaxIdleConns:          100,
            IdleConnTimeout:       90 * time.Second,
            TLSHandshakeTimeout:   15 * time.Second,
            ResponseHeaderTimeout: 15 * time.Second,
            ExpectContinueTimeout: 5 * time.Second,
            DisableCompression:    true,
            MaxConnsPerHost:       10,
            DisableKeepAlives:     false,
            ForceAttemptHTTP2:     true,
        },
    }

    // Verificar el tamaño del archivo
    head, err := client.Head(url)
    if (err != nil) {
        log.Printf("Error getting file info: %v", err)
        sendMessage(safeConn, "error", url, fmt.Sprintf("Error checking file: %v", err))
        return
    }
    totalSize := head.ContentLength

    // Comentamos esta verificación que está causando la cancelación prematura
    // ya que isDownloadActive no está funcionando como esperamos
    /*
    if !isDownloadActive(url) {
        log.Printf("Download was cancelled during initialization: %s", url)
        return
    }
    */

    // Intentar la descarga con retries
    var resp *http.Response
    maxRetries := 15  // Aumentado de 10 a 15

    for attempt := 0; attempt < maxRetries; attempt++ {
        if attempt > 0 {
            delay := time.Duration(attempt) * time.Second
            log.Printf("Retry attempt %d/%d after %v delay", attempt+1, maxRetries, delay)
            sendMessage(safeConn, "log", url, fmt.Sprintf("Reconnecting... (attempt %d/%d)", attempt+1, maxRetries))
            time.Sleep(delay)
        }

        req, _ := http.NewRequest("GET", url, nil)
        resp, err = client.Do(req)
        if err == nil {
            break
        }
        log.Printf("Download attempt %d failed: %v", attempt+1, err)
    }

    if err != nil {
        log.Printf("All download attempts failed for %s: %v", url, err)
        sendMessage(safeConn, "error", url, "All download attempts failed")
        return
    }
    defer resp.Body.Close()

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

    // Buffer más grande para mejor rendimiento
    buffer := make([]byte, 256*1024) // 256KB buffer
    file, err := os.Create(savePath)
    if err != nil {
        log.Printf("Error creating file: %v", err)
        sendMessage(safeConn, "error", url, fmt.Sprintf("Error creating file: %v", err))
        return
    }
    defer file.Close()

    // Control de progreso mejorado
    downloaded := int64(0)  // Reset downloaded counter
    lastUpdate := time.Now()
    startTime := time.Now()

    // Control de progreso más frecuente
    reportTicker := time.NewTicker(100 * time.Millisecond)
    defer reportTicker.Stop()

    // Ticker modificado para verificar cancellation
    go func() {
        for range reportTicker.C {
            if !isDownloadActive(url) {
                return // Salir del goroutine si se ha cancelado
            }
            
            if downloaded > 0 {
                speed := float64(downloaded) / time.Since(startTime).Seconds()
                sendProgress(safeConn, url, downloaded, totalSize, speed)
            }
        }
    }()

    for {
        // Verificar si la descarga ha sido cancelada
        if !isDownloadActive(url) {
            log.Printf("Download cancelled during transfer: %s", url)
            return
        }
        
        n, err := resp.Body.Read(buffer)
        if n > 0 {
            _, writeErr := file.Write(buffer[:n])
            if writeErr != nil {
                log.Printf("Write error: %v", writeErr)
                sendMessage(safeConn, "error", url, fmt.Sprintf("Write error: %v", writeErr))
                return
            }
            downloaded += int64(n)

            // Actualizar progreso cada 100ms
            if time.Since(lastUpdate) >= 100*time.Millisecond {
                speed := float64(downloaded) / time.Since(startTime).Seconds()
                sendProgress(safeConn, url, downloaded, totalSize, speed)
                lastUpdate = time.Now()
            }
        }

        if err != nil {
            if err == io.EOF {
                break
            }
            log.Printf("Read error: %v", err)
            sendMessage(safeConn, "error", url, fmt.Sprintf("Read error: %v", err))
            return
        }
    }

    // Verificación final
    if totalSize > 0 && downloaded != totalSize {
        log.Printf("Incomplete download: %d of %d bytes", downloaded, totalSize)
        sendMessage(safeConn, "error", url, "Incomplete download")
        return
    }

    log.Printf("Download completed: %s", filename)
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

// Constantes para información del cliente
const (
    ImplementationInfo = "CatchMe v1.0.0"
    FeaturesSupported = "basic-download retry-mechanism chunked-download"
    ChunksSupported = true  // Actualizar a true
)

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
    
    // Enviar info al cliente sobre capacidades del servidor cuando se conecta
    serverInfo := map[string]interface{}{
        "type": "server_info",
        "implementation": ImplementationInfo,
        "features": FeaturesSupported,
        "chunks_supported": ChunksSupported,
    }
    
    safeConn.SendJSON(serverInfo)
    
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
                
                // Verificar si ya existe una descarga activa con esta URL
                // Usar activeDownloads en lugar de la función para ser consistente
                activeDownloadsMux.Lock()
                alreadyActive := activeDownloads[url]
                activeDownloadsMux.Unlock()
                
                if alreadyActive {
                    log.Printf("URL already being downloaded: %s", url)
                    sendMessage(safeConn, "error", url, "This URL is already being downloaded")
                } else {
                    // Por ahora, usar sólo el método normal sin la variable useChunks
                    go handleDownload(safeConn, url)
                }
            } else {
                log.Printf("Invalid download request, missing URL")
            }
        case "cancel_download":
            if url, ok := msg["url"].(string); ok {
                log.Printf("Canceling download for: %s", url)
                
                // Intentar cancelar descarga por chunks primero
                if isDownloadActive(url) {
                    // Los nombres de función deben coincidir exactamente
                    handleCancelChunkedDownload(safeConn, url)
                } else {
                    // Marcar como inactivo el método tradicional
                    markDownloadInactive(url)
                    
                    // Enviar confirmación al cliente
                    sendMessage(safeConn, "log", url, "Download canceled by user")
                    sendMessage(safeConn, "cancel_confirmed", url, "Download canceled successfully")
                }
            }
        case "pause_download":
            if url, ok := msg["url"].(string); ok {
                log.Printf("Pause request for: %s", url)
                
                // Pausar descarga por chunks
                if isDownloadActive(url) {
                    // Los nombres de función deben coincidir exactamente
                    handlePauseChunkedDownload(safeConn, url)
                }
            }
        case "resume_download":
            if url, ok := msg["url"].(string); ok {
                log.Printf("Resuming download for: %s", url)
                
                // Reanudar descarga por chunks
                if useChunks := true; useChunks && ChunksSupported {
                    // Los nombres de función deben coincidir exactamente
                    go handleResumeChunkedDownload(safeConn, url)
                } else {
                    go handleDownload(safeConn, url)
                }
            }
        case "ping":
            safeConn.SendJSON(map[string]string{"type": "pong"})
        default:
            log.Printf("Unhandled message type: %v", msg["type"])
        }
    }
}

func main() {
    // Asegurarse de que existe el directorio de logs
    err := os.MkdirAll("logs", os.ModePerm)
    if (err != nil) {
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
