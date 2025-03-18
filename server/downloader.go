package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
	"crypto/sha256"
	"io"
	"log"
)

// Gestor de descargas activas
var (
	activeDownloadsMap    = make(map[string]*ChunkedDownload)
	activeDownloadsMutex  sync.RWMutex
)

// Constantes de configuración
const (
	DefaultChunkSize   int64 = 10 * 1024 * 1024  // Aumentar a 10MB por defecto (era 5MB)
	MaxConcurrentChunks      = 5                 // Aumentar a 5 chunks concurrentes (era 3)
)

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
	// Comentar la variable supportsRanges ya que no se usa
	// supportsRanges := false
	acceptRanges := resp.Header.Get("Accept-Ranges")
	if acceptRanges == "bytes" {
		// supportsRanges = true
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

	// Crear instancia de descarga
	download := NewChunkedDownload(url, filename, contentLength, DefaultChunkSize)

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
	}
	download.mu.RUnlock()

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
				DisableKeepAlives:     false,        // Asegurar que keep-alives esté habilitado
				MaxConnsPerHost:       10,           // Aumentar conexiones por host
				ResponseHeaderTimeout: 15 * time.Second,
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

		// Obtener ruta destino
		home, err := os.UserHomeDir()
		if err != nil {
			sendMessage(safeConn, "error", url, fmt.Sprintf("Failed to get home directory: %v", err))
			return
		}
		downloadDir := filepath.Join(home, "Downloads")
		destPath := filepath.Join(downloadDir, filename)

		// Crear directorio de descargas si no existe
		if err := os.MkdirAll(downloadDir, 0755); err != nil {
			sendMessage(safeConn, "error", url, fmt.Sprintf("Failed to create download directory: %v", err))
			return
		}

		// Unir todos los chunks
		sendMessage(safeConn, "log", url, "Merging chunks...")
		if err := download.MergeChunks(destPath); err != nil {
			sendMessage(safeConn, "error", url, fmt.Sprintf("Failed to merge chunks: %v", err))
			return
		}

		// Limpiar archivos temporales
		if err := download.Cleanup(); err != nil {
			sendMessage(safeConn, "log", url, fmt.Sprintf("Warning: Failed to clean temporary files: %v", err))
		}

		// Notificar completado
		downloaded, total := download.GetProgress()
		sendProgress(safeConn, url, downloaded, total, 0, "completed")
		sendMessage(safeConn, "log", url, "Download completed successfully")
	}()
}

// Mejorar la función pauseChunkedDownload para enviar una confirmación más clara
func pauseChunkedDownload(safeConn *SafeConn, url string) {
	activeDownloadsMutex.RLock()
	download, exists := activeDownloadsMap[url]
	activeDownloadsMutex.RUnlock()

	if !exists {
		sendMessage(safeConn, "error", url, "No active download found to pause")
		return
	}

	// Pausar todos los chunks
	download.PauseAllChunks()
	
	// Enviar mensaje detallado
	sendMessage(safeConn, "log", url, "Download paused by user")
	
	// Notificar progreso actual para actualizar UI
	downloaded, total := download.GetProgress()
	progress := map[string]interface{}{
		"type":          "progress",
		"url":           url,
		"bytesReceived": downloaded,
		"totalBytes":    total,
		"speed":         0,
		"status":        "paused",
	}
	safeConn.SendJSON(progress)
	
	// Reportar estado actual de chunks para la UI
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
				Speed:    0,
			},
		})
		chunk.mu.Unlock()
	}
	download.mu.RUnlock()
}

// Mejorar resumeChunkedDownload para asegurar una reanudación apropiada
func resumeChunkedDownload(safeConn *SafeConn, url string) {
	// Verificar si existe una descarga pausada primero
	activeDownloadsMutex.RLock()
	download, exists := activeDownloadsMap[url]
	activeDownloadsMutex.RUnlock()
	
	if exists {
		// Si existe, actualizar estado y continuar con los chunks existentes
		sendMessage(safeConn, "log", url, "Resuming existing download")
		
		// Iniciar nuevos goroutines para chunks pausados
		go func() {
			// Cliente HTTP para las descargas
			downloadClient := &http.Client{
				Timeout: 0, // Sin timeout
				Transport: &http.Transport{
					MaxIdleConns:        100,
					IdleConnTimeout:     90 * time.Second,
					DisableCompression:  true,
					ForceAttemptHTTP2:   true,
				},
			}
			
			// Reanudar chunks pausados
			var wg sync.WaitGroup
			sem := make(chan struct{}, MaxConcurrentChunks)
			
			download.mu.RLock()
			for _, chunk := range download.Chunks {
				chunk.mu.Lock()
				if chunk.Status == ChunkPaused {
					currentChunk := chunk
					chunk.Status = ChunkPending // Marcar como pendiente para reiniciar
					chunk.cancelCtx = make(chan struct{}) // Nuevo canal
					sem <- struct{}{}
					wg.Add(1)
					go func() {
						defer func() {
							<-sem
							wg.Done()
						}()
						download.DownloadChunk(downloadClient, currentChunk, safeConn)
					}()
				}
				chunk.mu.Unlock()
			}
			download.mu.RUnlock()
			
			// Esperar a que terminen todos los chunks
			wg.Wait()
			
			// Verificar si se completó la descarga
			if download.IsComplete() {
				// Completar la descarga
				handleCompletedDownload(safeConn, url, download)
			}
		}()
	} else {
		// No existe, iniciar como nueva descarga
		startChunkedDownload(safeConn, url)
	}
}

// Nueva función para manejar descargas completadas
func handleCompletedDownload(safeConn *SafeConn, url string, download *ChunkedDownload) {
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
	
	// Unir chunks
	sendMessage(safeConn, "log", url, "Merging chunks...")
	if err := download.MergeChunks(destPath); err != nil {
		sendMessage(safeConn, "error", url, fmt.Sprintf("Failed to merge chunks: %v", err))
		return
	}
	
	// Limpiar temporales
	if err := download.Cleanup(); err != nil {
		sendMessage(safeConn, "log", url, fmt.Sprintf("Warning: Failed to clean temp files: %v", err))
	}
	
	// Notificar completado
	downloaded, total := download.GetProgress()
	sendProgress(safeConn, url, downloaded, total, 0, "completed")
	sendMessage(safeConn, "log", url, "Download completed successfully")
	
	// Eliminar del mapa de descargas activas
	activeDownloadsMutex.Lock()
	delete(activeDownloadsMap, url)
	activeDownloadsMutex.Unlock()
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

// getDownloadProgress obtiene el progreso actual de una descarga
func getDownloadProgress(url string) (downloaded int64, total int64, ok bool) {
	activeDownloadsMutex.RLock()
	download, exists := activeDownloadsMap[url]
	activeDownloadsMutex.RUnlock()

	if !exists {
		return 0, 0, false
	}

	downloaded, total = download.GetProgress()
	return downloaded, total, true
}

// isDownloadActive verifica si una URL está siendo descargada
func isDownloadActive(url string) bool {
    // Primero verificar el map de activeDownloads original
    activeDownloadsMux.Lock()
    exists := activeDownloads[url]
    activeDownloadsMux.Unlock()
    
    if exists {
        return true
    }
    
    // Si no está en el mapa original, verificar en activeDownloadsMap
    activeDownloadsMutex.RLock()
    _, existsInMap := activeDownloadsMap[url]
    activeDownloadsMutex.RUnlock()
    
    return existsInMap
}

// Asegurarse de que markDownloadActive y markDownloadInactive sean coherentes con isDownloadActive
func markChunkDownloadActive(url string) {
    // Marcar activo en ambos sistemas
    activeDownloadsMux.Lock()
    activeDownloads[url] = true
    activeDownloadsMux.Unlock()
    fmt.Printf("Chunk download tracked: %s\n", url)
}

func markChunkDownloadInactive(url string) {
    // Eliminar de ambos sistemas
    activeDownloadsMux.Lock()
    delete(activeDownloads, url)
    activeDownloadsMux.Unlock()
    
    activeDownloadsMutex.Lock()
    delete(activeDownloadsMap, url)
    activeDownloadsMutex.Unlock()
    
    fmt.Printf("Chunk download untracked: %s\n", url)
}

// Nueva función para calcular SHA-256 del archivo descargado
func calculateSHA256(filePath string) (string, error) {
    file, err := os.Open(filePath)
    if (err != nil) {
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
        start := time.Now()
        
        sendMessage(safeConn, "log", url, "Calculating SHA-256 checksum on server...")
        
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
        
        sendMessage(safeConn, "log", url, fmt.Sprintf("Checksum calculation completed in %.2fs", duration.Seconds()))
    }()
}
