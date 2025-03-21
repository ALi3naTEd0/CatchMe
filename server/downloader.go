package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Gestor de descargas activas
var (
	activeDownloadsMap   = make(map[string]*ChunkedDownload)
	activeDownloadsMutex sync.RWMutex
)

// Estructura para hacer seguimiento del estado de descargas
type downloadState struct {
	active bool
	paused bool
}

// Cambiamos el mapa para almacenar estados más complejos
var (
	activeDownloadsState = make(map[string]downloadState)
	activeDownloadsMux   sync.Mutex
)

// Constantes de configuración
const (
	DefaultChunkSize    int64 = 30 * 1024 * 1024 // Aumentar a 30MB por chunk (antes era 10MB)
	MaxConcurrentChunks       = 8                // Aumentar a 8 chunks concurrentes (antes era 5)
	MinChunkSize        int64 = 5 * 1024 * 1024  // 5MB mínimo
	MaxChunkSize        int64 = 50 * 1024 * 1024 // 50MB máximo

	// Auto-tune chunk size based on connection speed
	SpeedThresholdFast   int64 = 10 * 1024 * 1024 // 10MB/s
	SpeedThresholdMedium int64 = 5 * 1024 * 1024  // 5MB/s
)

// Speed tracking
var (
	speedHistory = make(map[string][]float64)
	speedMutex   sync.RWMutex
)

// Get previous speed for a URL
func getPreviousSpeed(url string) float64 {
	speedMutex.RLock()
	defer speedMutex.RUnlock()

	if speeds, exists := speedHistory[url]; exists && len(speeds) > 0 {
		// Calculate average of last 5 speed samples
		count := min(len(speeds), 5)
		sum := 0.0
		for i := len(speeds) - count; i < len(speeds); i++ {
			sum += speeds[i]
		}
		return sum / float64(count)
	}
	return 0
}

// Update speed history for a URL
func updateSpeedHistory(url string, speed float64) {
	speedMutex.Lock()
	defer speedMutex.Unlock()

	if _, exists := speedHistory[url]; !exists {
		speedHistory[url] = make([]float64, 0, 10)
	}

	// Add new speed
	speedHistory[url] = append(speedHistory[url], speed)

	// Keep only last 10 samples
	if len(speedHistory[url]) > 10 {
		speedHistory[url] = speedHistory[url][1:]
	}
}

// Helper function for min of two ints
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// handleChunkedDownload inicia una descarga por chunks (función de proxy con nombre que coincide con main.go)
func handleChunkedDownload(safeConn *SafeConn, url string) {
	startChunkedDownload(safeConn, url)
}

// handleCancelChunkedDownload cancela una descarga en progreso (función de proxy con nombre que coincide con main.go)
func handleCancelChunkedDownload(safeConn *SafeConn, url string) {
	cancelChunkedDownload(safeConn, url)
}

// handlePauseChunkedDownload pausa una descarga en progreso (función de proxy con nombre que coincide con main.go)
func handlePauseChunkedDownload(safeConn *SafeConn, url string) {
	pauseChunkedDownload(safeConn, url)
}

// handleResumeChunkedDownload reanuda una descarga pausada (función de proxy con nombre que coincide con main.go)
func handleResumeChunkedDownload(safeConn *SafeConn, url string) {
	resumeChunkedDownload(safeConn, url)
}

// startChunkedDownload inicia una descarga por chunks
func startChunkedDownload(safeConn *SafeConn, url string) {
	// Agregar tracking en el sistema principal
	markDownloadActive(url)
	defer markDownloadInactive(url)

	// Verificar si ya existe una descarga para esta URL
	activeDownloadsMutex.RLock()
	if _, exists := activeDownloadsMap[url]; exists {
		activeDownloadsMutex.RUnlock()
		sendMessage(safeConn, "error", url, "Download already in progress")
		return
	}
	activeDownloadsMutex.RUnlock()

	// Obtener información del archivo
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Head(url)
	if err != nil {
		sendMessage(safeConn, "error", url, fmt.Sprintf("Failed to get file info: %v", err))
		return
	}

	// Verificar si el servidor soporta rangos
	acceptRanges := resp.Header.Get("Accept-Ranges")
	if acceptRanges == "bytes" {
		sendMessage(safeConn, "log", url, "Server supports range requests, enabling chunked download")
	} else {
		sendMessage(safeConn, "log", url, "Server doesn't support range requests, using single connection")
	}

	// Obtener tamaño del archivo
	contentLength := resp.ContentLength
	if contentLength <= 0 {
		sendMessage(safeConn, "error", url, "Unable to determine file size")
		return
	}
	sendMessage(safeConn, "log", url, fmt.Sprintf("File size: %d bytes", contentLength))

	// Determinar nombre de archivo
	filename := filepath.Base(url)
	sendMessage(safeConn, "log", url, fmt.Sprintf("Downloading file: %s", filename))

	// Crear instancia de descarga con tamaño de chunk dinámico
	chunkSize := DefaultChunkSize
	if previousSpeed := getPreviousSpeed(url); previousSpeed > 0 {
		chunkSize = calculateOptimalChunkSize(previousSpeed)
	}
	download := NewChunkedDownload(url, filename, contentLength, chunkSize)

	// Preparar chunks
	if err := download.PrepareChunks(); err != nil {
		sendMessage(safeConn, "error", url, fmt.Sprintf("Failed to prepare chunks: %v", err))
		return
	}

	// Numerar y registrar chunks
	numChunks := len(download.Chunks)
	sendMessage(safeConn, "log", url, fmt.Sprintf("Split into %d chunks", numChunks))

	// Registrar la descarga
	activeDownloadsMutex.Lock()
	activeDownloadsMap[url] = download
	activeDownloadsMutex.Unlock()

	// Asegurar que eliminamos la descarga en caso de error
	defer func() {
		if r := recover(); r != nil {
			sendMessage(safeConn, "error", url, fmt.Sprintf("Download crashed: %v", r))
			activeDownloadsMutex.Lock()
			delete(activeDownloadsMap, url)
			activeDownloadsMutex.Unlock()
		}
	}()

	// Reportar estado inicial
	download.mu.RLock()
	// Enviar un progreso inicial de 0%
	sendProgress(safeConn, url, 0, contentLength, 0, "starting")

	// Un pequeño delay para permitir que la UI actualice
	time.Sleep(100 * time.Millisecond)

	// Luego reportar los chunks
	for _, chunk := range download.Chunks {
		safeConn.SendJSON(map[string]interface{}{
			"type": "chunk_init",
			"url":  url,
			"chunk": ChunkProgress{
				ID:     chunk.ID,
				Start:  chunk.Start,
				End:    chunk.End,
				Status: chunk.Status,
			},
		})
		// Pequeño delay entre chunks para no saturar
		time.Sleep(5 * time.Millisecond)
	}
	download.mu.RUnlock()

	// Otro pequeño delay antes de comenzar la descarga real
	time.Sleep(100 * time.Millisecond)

	// Iniciar proceso de descarga en background
	go func() {
		defer func() {
			// Asegurar que eliminamos la descarga al terminar
			activeDownloadsMutex.Lock()
			delete(activeDownloadsMap, url)
			activeDownloadsMutex.Unlock()
		}()

		// Cliente HTTP para las descargas - optimizado para mejor rendimiento
		downloadClient := &http.Client{
			Timeout: 0, // Sin timeout
			Transport: &http.Transport{
				MaxIdleConns:          100,
				IdleConnTimeout:       90 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
				DisableCompression:    true,
				ForceAttemptHTTP2:     true,
				DisableKeepAlives:     false,            // Asegurar que keep-alives esté habilitado
				MaxConnsPerHost:       20,               // Aumentar conexiones por host (antes 10)
				ResponseHeaderTimeout: 30 * time.Second, // Aumentar timeout (antes 15s)
				TLSHandshakeTimeout:   10 * time.Second,
			},
		}

		// Usar un WaitGroup en lugar de errgroup
		var wg sync.WaitGroup
		sem := make(chan struct{}, MaxConcurrentChunks)
		var downloadError error
		var errorMutex sync.Mutex

		// Iniciar descarga para cada chunk
		for _, chunk := range download.Chunks {
			currentChunk := chunk // Importante para evitar capturas de variables incorrectas
			sem <- struct{}{}     // Adquirir un slot
			wg.Add(1)
			go func() {
				defer func() {
					<-sem // Liberar slot al terminar
					wg.Done()
				}()
				if err := download.DownloadChunk(downloadClient, currentChunk, safeConn); err != nil {
					errorMutex.Lock()
					downloadError = err
					errorMutex.Unlock()
				}
			}()
		}

		// Esperar a que todos los chunks se completen
		wg.Wait()

		if downloadError != nil {
			sendMessage(safeConn, "error", url, fmt.Sprintf("Download failed: %v", downloadError))
			return
		}

		// Verificar si todos los chunks están completos
		if !download.IsComplete() {
			sendMessage(safeConn, "error", url, "Download incomplete")
			return
		}

		// 1. Primero enviar progreso 100%
		sendProgress(safeConn, url, download.Size, download.Size, 0, "completed")
		sendMessage(safeConn, "log", url, "📥 100.0%")

		// 2. Obtener ruta y crear directorio
		home, err := os.UserHomeDir()
		if err != nil {
			sendMessage(safeConn, "error", url, fmt.Sprintf("Failed to get home directory: %v", err))
			return
		}
		downloadDir := filepath.Join(home, "Downloads")
		destPath := filepath.Join(downloadDir, filename)

		if err := os.MkdirAll(downloadDir, 0755); err != nil {
			sendMessage(safeConn, "error", url, fmt.Sprintf("Failed to create download directory: %v", err))
			return
		}

		// 3. Merge chunks
		sendMessage(safeConn, "log", url, "🔄 Merging chunks...")
		if err := download.MergeChunks(destPath); err != nil {
			sendMessage(safeConn, "error", url, fmt.Sprintf("Failed to merge chunks: %v", err))
			return
		}

		// 4. Completado y SHA
		sendMessage(safeConn, "log", url, "✅ Download completed successfully")
		handleCalculateChecksum(safeConn, url, filename)

		// 5. Cleanup
		if err := download.Cleanup(); err != nil {
			sendMessage(safeConn, "log", url, fmt.Sprintf("Warning: Failed to clean temporary files: %v", err))
		}
	}()
}

// Función mejorada para pausar una descarga por chunks
func pauseChunkedDownload(safeConn *SafeConn, url string) {
	log.Printf("Server: Pausing download: %s", url)

	// First update speed history before pausing
	if download, exists := activeDownloadsMap[url]; exists {
		downloaded, _ := download.GetProgress() // Remove unused total variable
		// Convert downloaded to float64 for speed calculation
		updateSpeedHistory(url, float64(downloaded))
	}

	// CRITICAL: Set paused state BEFORE sending pause to chunks
	activeDownloadsMutex.RLock()
	download, exists := activeDownloadsMap[url]
	activeDownloadsMutex.RUnlock()

	if !exists {
		log.Printf("No chunked download found to pause for: %s", url)
		// Enviar confirmación de todas formas para mantener la UI consistente
		sendMessage(safeConn, "pause_confirmed", url, "Download paused successfully")
		return
	}

	log.Printf("Pausing chunked download: %s", url)

	// Marcar la descarga como pausada y pausar todos los chunks
	download.mu.Lock()
	download.Paused = true
	download.mu.Unlock()

	// Pausar todos los chunks y esperar confirmación
	download.PauseAllChunks()

	// Actualizar estado global DESPUÉS de pausar los chunks
	activeDownloadsMux.Lock()
	activeDownloadsState[url] = downloadState{active: true, paused: true}
	activeDownloadsMux.Unlock()

	// Enviar mensaje detallado de log
	sendMessage(safeConn, "log", url, "Download paused successfully by server")

	// Notificar progreso actual para actualizar UI
	downloaded, total := download.GetProgress()

	// IMPORTANTE: Enviar mensaje de pausa confirmada PRIMERO
	sendMessage(safeConn, "pause_confirmed", url, "Download paused successfully")
	// Luego enviar actualización de progreso
	progress := map[string]interface{}{
		"type":          "progress",
		"url":           url,
		"bytesReceived": downloaded,
		"totalBytes":    total,
		"speed":         0,
		"status":        "paused",
	}
	safeConn.SendJSON(progress)

	// Reportar estado actual de todos los chunks para la UI
	download.mu.RLock()
	for _, chunk := range download.Chunks {
		chunk.mu.Lock()
		safeConn.SendJSON(map[string]interface{}{
			"type": "chunk_progress",
			"url":  url,
			"chunk": ChunkProgress{
				ID:       chunk.ID,
				Start:    chunk.Start,
				End:      chunk.End,
				Progress: chunk.Progress,
				Status:   chunk.Status,
				Speed:    0, // Velocidad cero al pausar
			},
		})
		chunk.mu.Unlock()
	}
	download.mu.RUnlock()
	log.Printf("Download paused successfully: %s", url)
}

// Función mejorada para reanudar una descarga por chunks
func resumeChunkedDownload(safeConn *SafeConn, url string) {
	log.Printf("Server: Resuming download: %s", url)

	activeDownloadsMutex.RLock()
	download, exists := activeDownloadsMap[url]
	activeDownloadsMutex.RUnlock()

	if !exists {
		log.Printf("No download found to resume: %s", url)
		sendMessage(safeConn, "error", url, "No download found to resume")
		return
	}

	// First update global state
	activeDownloadsMux.Lock()
	activeDownloadsState[url] = downloadState{active: true, paused: false}
	activeDownloadsMux.Unlock()

	// Reset download state
	download.mu.Lock()
	download.Paused = false
	download.mu.Unlock()

	// Send initial resume confirmation
	sendMessage(safeConn, "resume_confirmed", url, "Download resumed successfully")

	// Create fresh HTTP client for resuming
	downloadClient := &http.Client{
		Timeout: 0,
		Transport: &http.Transport{
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			DisableCompression:    true,
			ForceAttemptHTTP2:     true,
			MaxConnsPerHost:       10,
			TLSHandshakeTimeout:   10 * time.Second,
			DisableKeepAlives:     false,
			ResponseHeaderTimeout: 30 * time.Second,
		},
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, MaxConcurrentChunks)
	var downloadError error
	var errorMutex sync.Mutex

	// Resume each non-completed chunk
	download.mu.RLock()
	for _, chunk := range download.Chunks {
		chunk.mu.Lock()
		if chunk.Status != ChunkCompleted {
			chunk.Status = ChunkPending
			chunk.cancelCtx = make(chan struct{})
			currentChunk := chunk
			chunk.mu.Unlock()

			sem <- struct{}{}
			wg.Add(1)
			go func() {
				defer func() {
					<-sem
					wg.Done()
				}()
				if err := download.DownloadChunk(downloadClient, currentChunk, safeConn); err != nil {
					errorMutex.Lock()
					downloadError = err
					errorMutex.Unlock()
				}
			}()
		} else {
			chunk.mu.Unlock()
		}
	}
	download.mu.RUnlock()

	// Wait for all chunks and handle completion
	go func() {
		wg.Wait()
		if downloadError != nil {
			sendMessage(safeConn, "error", url, fmt.Sprintf("Resume failed: %v", downloadError))
			return
		}
		if download.IsComplete() {
			handleCompletedDownload(safeConn, url, download)
		}
	}()
}

// Nueva función para manejar descargas completadas
func handleCompletedDownload(safeConn *SafeConn, url string, download *ChunkedDownload) {
	downloaded, total := download.GetProgress()
	if total-downloaded > 1024 {
		sendMessage(safeConn, "error", url, "Download incomplete")
		return
	}

	// Force exact sequence:
	// 1. Send completion status with force flag
	safeConn.SendJSON(map[string]interface{}{
		"type":    "final_completion",
		"url":     url,
		"message": "📥 100.0%",
		"force":   true,
	})
	time.Sleep(500 * time.Millisecond)

	// 2. Send progress update to ensure UI reflects 100%
	safeConn.SendJSON(map[string]interface{}{
		"type":          "progress",
		"url":           url,
		"bytesReceived": total,
		"totalBytes":    total,
		"status":        "completed",
	})
	time.Sleep(500 * time.Millisecond)

	// 3. Send merge message
	safeConn.SendJSON(map[string]interface{}{
		"type":    "merge_start",
		"url":     url,
		"message": "🔄 Merging chunks...",
	})

	// Obtener ruta destino
	home, err := os.UserHomeDir()
	if err != nil {
		sendMessage(safeConn, "error", url, fmt.Sprintf("Failed to get home directory: %v", err))
		return
	}
	downloadDir := filepath.Join(home, "Downloads")
	destPath := filepath.Join(downloadDir, download.Filename)

	// Crear directorio si no existe
	if err := os.MkdirAll(downloadDir, 0755); err != nil {
		sendMessage(safeConn, "error", url, fmt.Sprintf("Failed to create download directory: %v", err))
		return
	}

	// Verificar tamaño
	downloaded, total = download.GetProgress()
	if total-downloaded > 1024 {
		sendMessage(safeConn, "error", url, "Download incomplete")
		return
	}

	// Proper completion sequence - consolidated
	total = download.Size

	// Force exact sequence:
	// 1. Set status to completed with exact progress
	safeConn.SendJSON(map[string]interface{}{
		"type":          "progress",
		"url":           url,
		"bytesReceived": total,
		"totalBytes":    total,
		"status":        "completed",
	})
	time.Sleep(200 * time.Millisecond)

	// 2. Send 100% message
	safeConn.SendJSON(map[string]interface{}{
		"type":    "log",
		"url":     url,
		"message": "📥 100.0%",
		"force":   true, // Add force flag
	})
	time.Sleep(500 * time.Millisecond)

	// Wait longer before merge to ensure UI updates
	time.Sleep(500 * time.Millisecond)

	// Then start merge process
	safeConn.SendJSON(map[string]interface{}{
		"type":    "merge_start",
		"url":     url,
		"message": "🔄 Merging chunks...",
	})

	if err := download.MergeChunks(destPath); err != nil {
		sendMessage(safeConn, "error", url, fmt.Sprintf("Failed to merge chunks: %v", err))
		return
	}

	// 3. Final completion messages
	sendMessage(safeConn, "log", url, "✅ Download completed successfully")
	handleCalculateChecksum(safeConn, url, download.Filename)
}

// cancelChunkedDownload cancela una descarga en progreso
func cancelChunkedDownload(safeConn *SafeConn, url string) {
	activeDownloadsMutex.RLock()
	download, exists := activeDownloadsMap[url]
	activeDownloadsMutex.RUnlock()

	if !exists {
		sendMessage(safeConn, "log", url, "No active download found to cancel")
		sendMessage(safeConn, "cancel_confirmed", url, "Download already cancelled")
		return
	}

	// Pausar todos los chunks para detener la descarga
	download.PauseAllChunks()

	// Eliminar del mapa de descargas activas
	activeDownloadsMutex.Lock()
	delete(activeDownloadsMap, url)
	activeDownloadsMutex.Unlock()

	// Limpiar archivos temporales
	if err := download.Cleanup(); err != nil {
		sendMessage(safeConn, "log", url, fmt.Sprintf("Warning: Failed to clean temporary files: %v", err))
	}

	sendMessage(safeConn, "log", url, "Download canceled")
	sendMessage(safeConn, "cancel_confirmed", url, "Download canceled successfully")
}

// isDownloadActive verifica si una URL está siendo descargada
func isDownloadActive(url string) bool {
	// Primero verificar el mapa de estados
	activeDownloadsMux.Lock()
	state, exists := activeDownloadsState[url]
	activeDownloadsMux.Unlock()

	if exists && state.active && !state.paused {
		return true
	}

	// Si no está en el mapa o está pausada, verificar en activeDownloadsMap
	activeDownloadsMutex.RLock()
	download, existsInMap := activeDownloadsMap[url]
	activeDownloadsMutex.RUnlock()

	return existsInMap && !download.Paused
}

// markDownloadActive ahora establece el estado completo
func markDownloadActive(url string) {
	activeDownloadsMux.Lock()
	activeDownloadsState[url] = downloadState{active: true, paused: false}
	activeDownloadsMux.Unlock()
	log.Printf("Download tracked: %s", url)
}

// markDownloadInactive limpia el estado
func markDownloadInactive(url string) {
	activeDownloadsMux.Lock()
	delete(activeDownloadsState, url)
	activeDownloadsMux.Unlock()
	log.Printf("Download untracked: %s", url)
}

// Nueva función para calcular SHA-256 del archivo descargado
func calculateSHA256(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("error opening file for checksum: %v", err)
	}
	defer file.Close()

	hash := sha256.New()

	// Usar un buffer grande para mejorar rendimiento
	buf := make([]byte, 8*1024*1024) // 8MB buffer

	start := time.Now()
	totalBytes := 0
	for {
		n, err := file.Read(buf)
		if n > 0 {
			totalBytes += n
			hash.Write(buf[:n])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("error reading file for checksum: %v", err)
		}
	}
	duration := time.Since(start)

	checksum := fmt.Sprintf("%x", hash.Sum(nil))
	log.Printf("SHA-256 checksum calculated in %v for %s: %s (processed %d bytes)",
		duration, filepath.Base(filePath), checksum, totalBytes)

	return checksum, nil
}

// handleCalculateChecksum procesa la solicitud de cálculo de checksum
// handleCalculateChecksum procesa la solicitud de cálculo de checksum
func handleCalculateChecksum(safeConn *SafeConn, url string, filename string) {
	log.Printf("Calculating checksum for: %s", filename)
	// Generar ruta del archivo
	home, err := os.UserHomeDir()
	if err != nil {
		sendMessage(safeConn, "error", url, fmt.Sprintf("Failed to get home directory: %v", err))
		return
	}
	filePath := filepath.Join(home, "Downloads", filename)

	// Verificar que el archivo existe
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		sendMessage(safeConn, "error", url, fmt.Sprintf("File not found for checksum: %v", err))
		return
	}

	// Iniciar el cálculo en una goroutine separada
	go func() {
		sendMessage(safeConn, "log", url, "🔐 Starting SHA-256 checksum calculation...")

		start := time.Now()
		checksum, err := calculateSHA256(filePath)
		if err != nil {
			sendMessage(safeConn, "error", url, fmt.Sprintf("Checksum calculation failed: %v", err))
			return
		}

		duration := time.Since(start)

		// Enviar resultado al cliente
		safeConn.SendJSON(map[string]interface{}{
			"type":     "checksum_result",
			"url":      url,
			"filename": filename,
			"checksum": checksum,
			"duration": duration.Milliseconds(),
		})

		// Este log es suficiente, no necesitamos otro mensaje adicional
		log.Printf("Checksum calculation done for %s: %s", filename, checksum)

		// IMPORTANTE: Asegurarse de que el item no sigue en ningún mapa
		activeDownloadsMutex.Lock()
		delete(activeDownloadsMap, url)
		activeDownloadsMutex.Unlock()

		activeDownloadsMux.Lock()
		delete(activeDownloadsState, url)
		activeDownloadsMux.Unlock()
	}()
}

func calculateOptimalChunkSize(speed float64) int64 {
	switch {
	case speed >= float64(SpeedThresholdFast):
		return MaxChunkSize
	case speed >= float64(SpeedThresholdMedium):
		return DefaultChunkSize
	default:
		return MinChunkSize
	}
}
