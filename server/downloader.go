package main

import (
	"context"
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

	// Retry settings
	MaxChunkRetries      = 5  // Maximum retries per chunk
	InitialRetryDelay    = 1  // Initial retry delay in seconds
	MaxRetryDelay        = 15 // Maximum retry delay in seconds
	DownloadTimeout      = 30 // Timeout for individual chunk operations in seconds
	StuckProgressTimeout = 60 // Consider a chunk stuck if no progress for this many seconds
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

	// Ensure all initial messages are sent with delays
	time.Sleep(100 * time.Millisecond)

	// Reportar estado inicial
	sendProgress(safeConn, url, 0, contentLength, 0, "starting")
	sendMessage(safeConn, "log", url, "📥 0.0%")
	time.Sleep(300 * time.Millisecond) // Longer delay for UI to reflect starting state

	// Luego reportar los chunks en un bloque de RLock
	download.mu.RLock()
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
		// Shorter delay between chunks
		time.Sleep(5 * time.Millisecond)
	}
	download.mu.RUnlock()

	// One final delay before starting download
	time.Sleep(200 * time.Millisecond)

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

		// SIMPLIFIED COMPLETION SEQUENCE with more robust error handling
		if download.IsComplete() {
			// Get destination path
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

			// STRICTLY ORDERED SEQUENCE:
			// 1. First check all chunks are really complete
			for _, chunk := range download.Chunks {
				chunk.mu.Lock()
				if chunk.Status != ChunkCompleted {
					errMsg := fmt.Sprintf("Chunk %d not completed (status: %s, progress: %d/%d)",
						chunk.ID, chunk.Status, chunk.Progress,
						chunk.End-chunk.Start+1)
					chunk.mu.Unlock()
					sendMessage(safeConn, "error", url, errMsg)
					return
				}
				chunk.mu.Unlock()
			}

			// 2. Send 99.9% progress
			sendProgress(safeConn, url, download.Size-1, download.Size, 0, "downloading")
			sendMessage(safeConn, "log", url, "📥 99.9%")
			time.Sleep(300 * time.Millisecond)

			// 3. Then 100% progress
			sendProgress(safeConn, url, download.Size, download.Size, 0, "completed")
			sendMessage(safeConn, "log", url, "📥 100.0%")
			time.Sleep(300 * time.Millisecond)

			// 4. Then merging message
			sendMessage(safeConn, "log", url, "🔄 Merging chunks...")

			// 5. Perform actual merge with retry
			var mergeErr error
			for attempt := 0; attempt < 3; attempt++ {
				if attempt > 0 {
					sendMessage(safeConn, "log", url, fmt.Sprintf("Retrying merge (attempt %d/3)...", attempt+1))
					time.Sleep(time.Second * time.Duration(attempt+1))
				}

				if err := download.MergeChunks(destPath); err != nil {
					mergeErr = err
					log.Printf("Merge attempt %d failed: %v", attempt+1, err)
				} else {
					mergeErr = nil
					break
				}
			}

			if mergeErr != nil {
				sendMessage(safeConn, "error", url, fmt.Sprintf("Failed to merge chunks: %v", mergeErr))
				return
			}

			time.Sleep(300 * time.Millisecond)

			// 6. Download completed message
			sendMessage(safeConn, "log", url, "✅ Download completed successfully")
			time.Sleep(300 * time.Millisecond)

			// 7. Calculate checksum (just once)
			handleCalculateChecksum(safeConn, url, filename)

			// 8. Cleanup temporary files in background to avoid blocking
			go func() {
				if err := download.Cleanup(); err != nil {
					log.Printf("Warning: Failed to clean temporary files: %v", err)
				}
			}()
		} else {
			// Add detailed error about incomplete chunks
			incompleteChunks := []int{}
			download.mu.RLock()
			for _, chunk := range download.Chunks {
				chunk.mu.Lock()
				if chunk.Status != ChunkCompleted {
					incompleteChunks = append(incompleteChunks, chunk.ID)
				}
				chunk.mu.Unlock()
			}
			download.mu.RUnlock()

			errorMsg := fmt.Sprintf("Download incomplete: %d/%d chunks not completed. IDs: %v",
				len(incompleteChunks), len(download.Chunks), incompleteChunks)
			sendMessage(safeConn, "error", url, errorMsg)
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

		// Replace handleCompletedDownload with direct completion handling
		if download.IsComplete() {
			// Get destination path
			home, err := os.UserHomeDir()
			if err != nil {
				sendMessage(safeConn, "error", url, fmt.Sprintf("Failed to get home directory: %v", err))
				return
			}
			downloadDir := filepath.Join(home, "Downloads")
			destPath := filepath.Join(downloadDir, download.Filename)

			if err := os.MkdirAll(downloadDir, 0755); err != nil {
				sendMessage(safeConn, "error", url, fmt.Sprintf("Failed to create download directory: %v", err))
				return
			}

			// STRICTLY ORDERED SEQUENCE:
			// 1. First send 99.9% progress
			sendProgress(safeConn, url, download.Size-1, download.Size, 0, "downloading")
			sendMessage(safeConn, "log", url, "📥 99.9%")
			time.Sleep(300 * time.Millisecond)

			// 2. Then 100% progress
			sendProgress(safeConn, url, download.Size, download.Size, 0, "completed")
			sendMessage(safeConn, "log", url, "📥 100.0%")
			time.Sleep(300 * time.Millisecond)

			// 3. Then merging message
			sendMessage(safeConn, "log", url, "🔄 Merging chunks...")

			// 4. Perform actual merge
			if err := download.MergeChunks(destPath); err != nil {
				sendMessage(safeConn, "error", url, fmt.Sprintf("Failed to merge chunks: %v", err))
				return
			}
			time.Sleep(300 * time.Millisecond)

			// 5. Download completed message
			sendMessage(safeConn, "log", url, "✅ Download completed successfully")
			time.Sleep(300 * time.Millisecond)

			// 6. Calculate checksum (just once)
			handleCalculateChecksum(safeConn, url, download.Filename)

			// 7. Cleanup temporary files
			if err := download.Cleanup(); err != nil {
				log.Printf("Warning: Failed to clean temporary files: %v", err)
			}
		}
	}()
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
	log.Printf("Download tracked: %s (active=%t, paused=%t)",
		url, true, false)
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

// DownloadChunk descarga un chunk específico - modificado para usar la nueva función con retry
func (d *ChunkedDownload) DownloadChunk(client *http.Client, chunk *Chunk, safeConn *SafeConn) error {
	// Reset chunk state at start
	chunk.mu.Lock()
	if chunk.Status != ChunkCompleted {
		chunk.Status = ChunkActive
	}
	chunk.mu.Unlock()

	// Añadir log de inicio de chunk
	log.Printf("Starting chunk %d: bytes %d-%d", chunk.ID, chunk.Start, chunk.End)

	if chunk.Status == ChunkCompleted {
		return nil
	}

	// Marcar como activo
	chunk.mu.Lock()
	chunk.Status = ChunkActive
	chunk.mu.Unlock()

	// Add retry loop with exponential backoff
	var lastError error
	retryCount := 0

	for retryCount <= MaxChunkRetries {
		if retryCount > 0 {
			// Calculate backoff with exponential increase capped at MaxRetryDelay
			delay := time.Duration(min(InitialRetryDelay<<uint(retryCount-1), MaxRetryDelay)) * time.Second
			log.Printf("Retrying chunk %d (attempt %d/%d) after %v delay",
				chunk.ID, retryCount, MaxChunkRetries, delay)

			// Send retry info to client
			if safeConn != nil {
				safeConn.SendJSON(map[string]interface{}{
					"type": "chunk_retry",
					"url":  d.URL,
					"chunk": ChunkProgress{
						ID:     chunk.ID,
						Start:  chunk.Start,
						End:    chunk.End,
						Status: "retrying",
					},
					"retry":       retryCount,
					"max_retries": MaxChunkRetries,
					"delay":       delay.Seconds(),
				})
			}

			time.Sleep(delay)
		}

		// Check if the download has been paused or canceled
		select {
		case <-chunk.cancelCtx:
			chunk.mu.Lock()
			if chunk.Status == ChunkActive {
				chunk.Status = ChunkPaused
			}
			chunk.mu.Unlock()
			return nil
		default:
			if d.Paused {
				chunk.mu.Lock()
				if chunk.Status == ChunkActive {
					chunk.Status = ChunkPaused
				}
				chunk.mu.Unlock()
				return nil
			}
		}

		// Try the download using our new timeout method
		err := d.tryDownloadChunkWithTimeout(client, chunk, safeConn)
		if err == nil {
			// Success!
			return nil
		}

		// Log the error and retry
		lastError = err
		log.Printf("Chunk %d download failed (attempt %d/%d): %v",
			chunk.ID, retryCount+1, MaxChunkRetries+1, err)

		// Increment retry count and continue
		retryCount++
	}

	// If we get here, all retries failed
	chunk.mu.Lock()
	chunk.Status = ChunkFailed
	chunk.Error = lastError.Error()
	chunk.mu.Unlock()

	return fmt.Errorf("chunk %d failed after %d retries: %v",
		chunk.ID, MaxChunkRetries, lastError)
}

// tryDownloadChunkWithTimeout handles downloading a chunk with timeout detection
func (d *ChunkedDownload) tryDownloadChunkWithTimeout(client *http.Client, chunk *Chunk, safeConn *SafeConn) error {
	// Crear o abrir archivo para el chunk
	file, err := os.OpenFile(chunk.Path, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open chunk file: %v", err)
	}
	defer file.Close()

	// Establecer posición inicial
	if chunk.Progress > 0 {
		if _, err := file.Seek(chunk.Progress, 0); err != nil {
			return fmt.Errorf("failed to seek in chunk file: %v", err)
		}
	}

	// Crear request con rango
	req, err := http.NewRequest("GET", d.URL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	// Establecer rango de bytes para este chunk
	rangeStart := chunk.Start + chunk.Progress
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", rangeStart, chunk.End))

	// Añadir User-Agent para evitar bloqueos/limitaciones
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/96.0.4664.93 Safari/537.36")

	// Add context with timeout to detect stuck downloads
	ctx, cancel := context.WithTimeout(context.Background(), DownloadTimeout*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	// Iniciar descarga
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to start download: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("server returned status code %d", resp.StatusCode)
	}

	// Verificar si el servidor soporta rangos
	if resp.StatusCode != http.StatusPartialContent {
		// Some servers don't return 206 but still honor range - try to continue
		log.Printf("Warning: Server didn't respond with 206 Partial Content, but trying to continue")
	}

	// Add progress monitoring with timeout detection
	startTime := time.Now()
	lastProgressTime := time.Now()
	lastProgress := chunk.Progress
	updateInterval := 100 * time.Millisecond
	buffer := make([]byte, 512*1024)
	lastUpdate := time.Now() // Define lastUpdate here to fix the undefined variable error

	// Create a channel for the download goroutine
	downloadDone := make(chan error, 1)

	// Start the download in a separate goroutine
	go func() {
		for {
			// Check if download has been canceled or paused
			select {
			case <-chunk.cancelCtx:
				downloadDone <- nil
				return
			default:
				if d.Paused {
					downloadDone <- nil
					return
				}
			}

			// Read data with timeout
			n, err := resp.Body.Read(buffer)
			if n > 0 {
				// Write to file
				_, writeErr := file.Write(buffer[:n])
				if writeErr != nil {
					downloadDone <- fmt.Errorf("write error: %v", writeErr)
					return
				}

				// Update progress
				chunk.mu.Lock()
				chunk.Progress += int64(n)
				currentProgress := chunk.Progress
				chunk.mu.Unlock()

				lastProgressTime = time.Now() // Update progress time

				// Send progress update at interval
				now := time.Now()
				if now.Sub(lastUpdate) >= updateInterval {
					elapsed := now.Sub(startTime).Seconds()
					if elapsed > 0 {
						speed := float64(currentProgress-lastProgress) / now.Sub(lastUpdate).Seconds()

						// Report progress with speed
						if safeConn != nil {
							d.mu.RLock()
							safeConn.SendJSON(map[string]interface{}{
								"type": "chunk_progress",
								"url":  d.URL,
								"chunk": ChunkProgress{
									ID:       chunk.ID,
									Start:    chunk.Start,
									End:      chunk.End,
									Progress: currentProgress,
									Status:   chunk.Status,
									Speed:    speed,
								},
							})

							// Also report overall progress
							downloaded, total := d.GetProgress()
							safeConn.SendJSON(map[string]interface{}{
								"type":          "progress",
								"url":           d.URL,
								"bytesReceived": downloaded,
								"totalBytes":    total,
								"speed":         speed,
							})
							d.mu.RUnlock()
						}

						lastUpdate = now
						lastProgress = currentProgress
					}
				}
			}

			if err != nil {
				if err == io.EOF {
					// Successfully completed
					chunk.markCompleted()

					// Report stats
					elapsed := time.Since(startTime)
					totalBytes := chunk.End - chunk.Start + 1
					avgSpeed := float64(totalBytes) / elapsed.Seconds()

					log.Printf("Chunk %d completed in %.2fs (%.2f MB/s)",
						chunk.ID, elapsed.Seconds(), avgSpeed/(1024*1024))

					// Send final notification
					if safeConn != nil {
						safeConn.SendJSON(map[string]interface{}{
							"type": "chunk_progress",
							"url":  d.URL,
							"chunk": ChunkProgress{
								ID:        chunk.ID,
								Start:     chunk.Start,
								End:       chunk.End,
								Progress:  totalBytes,
								Status:    ChunkCompleted,
								Speed:     0,
								Completed: chunk.End + 1,
							},
						})
					}

					downloadDone <- nil
					return
				}

				// Other error - signal failure
				downloadDone <- err
				return
			}

			// Check if download is stuck (no progress for a while)
			if time.Since(lastProgressTime) > StuckProgressTimeout*time.Second {
				downloadDone <- fmt.Errorf("download stuck - no progress for %d seconds", StuckProgressTimeout)
				return
			}
		}
	}()

	// Wait for download completion or timeout
	select {
	case err := <-downloadDone:
		return err
	case <-ctx.Done():
		// Timeout occurred
		return fmt.Errorf("download timeout after %d seconds", DownloadTimeout)
	}
}
