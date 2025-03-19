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

// Estructura para hacer seguimiento del estado de descargas
type downloadState struct {
    active bool
    paused bool
}

// Cambiamos el mapa para almacenar estados más complejos
var (
    activeDownloadsState = make(map[string]downloadState)
    activeDownloadsMux  sync.Mutex
)

// Constantes de configuración
const (
	DefaultChunkSize   int64 = 30 * 1024 * 1024  // Aumentar a 30MB por chunk (antes era 10MB)
	MaxConcurrentChunks      = 8                 // Aumentar a 8 chunks concurrentes (antes era 5)
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
				MaxConnsPerHost:       20,           // Aumentar conexiones por host (antes 10)
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

// Función mejorada para pausar una descarga por chunks
func pauseChunkedDownload(safeConn *SafeConn, url string) {
    log.Printf("Server: Pausing download: %s", url)
    
    activeDownloadsMutex.RLock()
    download, exists := activeDownloadsMap[url]
    activeDownloadsMutex.RUnlock()

    if !exists {
        // También verificar en el mapa tradicional
        activeDownloadsMux.Lock()
        state, exists := activeDownloadsState[url]
        activeDownloadsMux.Unlock()
        
        if (!exists || !state.active) {
            log.Printf("No download found to pause: %s", url)
            sendMessage(safeConn, "error", url, "No active download found to pause")
            return
        }
        
        // Si está en el mapa tradicional pero no en el de chunks
        // Necesitamos detener la descarga tradicional
        activeDownloadsMux.Lock()
        state.paused = true
        activeDownloadsState[url] = state
        activeDownloadsMux.Unlock()
        
        // Confirmar la pausa al cliente
        sendMessage(safeConn, "pause_confirmed", url, "Download paused successfully")
        return
    }

    // Pausar todos los chunks
    download.PauseAllChunks()
    
    // Enviar mensaje detallado de log
    sendMessage(safeConn, "log", url, "Download paused by server")
    
    // Notificar progreso actual para actualizar UI
    downloaded, total := download.GetProgress()
    
    // Enviar mensaje de pausa confirmada PRIMERO
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
    
    log.Printf("Download paused: %s", url)
}

// Función mejorada para reanudar una descarga por chunks
func resumeChunkedDownload(safeConn *SafeConn, url string) {
    log.Printf("Server: Resuming download: %s", url)
    
    // Primero enviar confirmación de reanudación (importante para la UI)
    sendMessage(safeConn, "resume_confirmed", url, "Download resumed successfully")
    
    // Verificar si existe una descarga pausada
    activeDownloadsMutex.RLock()
    download, exists := activeDownloadsMap[url]
    activeDownloadsMutex.RUnlock()
    
    if exists {
        log.Printf("Found existing download to resume: %s", url)
        
        // Si existe, actualizar estado y continuar con los chunks existentes
        sendMessage(safeConn, "log", url, "Resuming existing download...")
        
        // Iniciar nuevos goroutines para chunks pausados
        go func() {
            // Cliente HTTP optimizado para las descargas
            downloadClient := &http.Client{
                Timeout: 0, // Sin timeout
                Transport: &http.Transport{
                    MaxIdleConns:        100,
                    IdleConnTimeout:     90 * time.Second,
                    DisableCompression:  true,
                    ForceAttemptHTTP2:   true,
                    MaxConnsPerHost:     10,
                    TLSHandshakeTimeout: 10 * time.Second,
                },
            }
            
            // Reanudar chunks pausados con concurrencia controlada
            var wg sync.WaitGroup
            sem := make(chan struct{}, MaxConcurrentChunks)
            
            download.mu.RLock()
            
            // Contar cuántos chunks necesitan reanudarse
            pendingChunks := 0
            for _, chunk := range download.Chunks {
                chunk.mu.Lock()
                if chunk.Status == ChunkPaused || chunk.Status == ChunkPending {
                    pendingChunks++
                }
                chunk.mu.Unlock()
            }
            
            // Informar cuántos chunks se van a reanudar
            if pendingChunks > 0 {
                sendMessage(safeConn, "log", url, fmt.Sprintf("Resuming %d chunks...", pendingChunks))
            }
            
            // Iniciar descarga de chunks pendientes
            for _, chunk := range download.Chunks {
                chunk.mu.Lock()
                if chunk.Status == ChunkPaused || chunk.Status == ChunkPending {
                    currentChunk := chunk
                    chunk.Status = ChunkPending // Marcar como pendiente para reiniciar
                    chunk.cancelCtx = make(chan struct{}) // Nuevo canal para poder cancelar
                    
                    // Informar al cliente que el chunk se está reanudando
                    safeConn.SendJSON(map[string]interface{}{
                        "type": "chunk_progress",
                        "url":  url,
                        "chunk": ChunkProgress{
                            ID:       currentChunk.ID,
                            Start:    currentChunk.Start,
                            End:      currentChunk.End,
                            Progress: currentChunk.Progress,
                            Status:   ChunkPending,
                        },
                    })
                    
                    sem <- struct{}{} // Adquirir slot de concurrencia
                    wg.Add(1)
                    go func() {
                        defer func() {
                            <-sem // Liberar slot
                            wg.Done()
                        }()
                        if err := download.DownloadChunk(downloadClient, currentChunk, safeConn); err != nil {
                            log.Printf("Error resuming chunk %d: %v", currentChunk.ID, err)
                        }
                    }()
                }
                chunk.mu.Unlock()
            }
            download.mu.RUnlock()
            
            // Esperar a que todos los chunks se completen
            go func() {
                wg.Wait()
                
                // Verificar si la descarga se completó
                if download.IsComplete() {
                    handleCompletedDownload(safeConn, url, download)
                } else {
                    // Si no se completó, verificar si algún chunk falló
                    downloadFailed := false
                    download.mu.RLock()
                    for _, chunk := range download.Chunks {
                        chunk.mu.Lock()
                        if chunk.Status == ChunkFailed {
                            downloadFailed = true
                        }
                        chunk.mu.Unlock()
                    }
                    download.mu.RUnlock()
                    
                    if downloadFailed {
                        sendMessage(safeConn, "error", url, "Some chunks failed to download")
                    }
                }
            }()
        }()
    } else {
        // Si no existe en el mapa de chunks, verificar en el mapa principal
        activeDownloadsMux.Lock()
        state, exists := activeDownloadsState[url] 
        activeDownloadsMux.Unlock()
        
        if exists && state.active && state.paused {
            // Si está en el mapa principal como pausada, quitar marca de pausa
            activeDownloadsMux.Lock()
            state.paused = false
            activeDownloadsState[url] = state
            activeDownloadsMux.Unlock()
            
            // Utilizar la descarga normal
            sendMessage(safeConn, "log", url, "Resuming standard download...")
            // No hacer nada, el mensaje resume_confirmed ya se envió
        } else {
            // No existe, iniciar como nueva descarga
            sendMessage(safeConn, "log", url, "Starting new download...")
            go startChunkedDownload(safeConn, url)
        }
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

	if (!exists) {
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

	if (!exists) {
		return 0, 0, false
	}

	downloaded, total = download.GetProgress()
	return downloaded, total, true
}

// isDownloadActive verifica si una URL está siendo descargada
func isDownloadActive(url string) bool {
    // Primero verificar el mapa de estados
    activeDownloadsMux.Lock()
    state, exists := activeDownloadsState[url]
    activeDownloadsMux.Unlock()
    
    if (exists && state.active && !state.paused) {
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

// markDownloadPaused marca la descarga como pausada
func markDownloadPaused(url string) {
    activeDownloadsMux.Lock()
    state := activeDownloadsState[url]
    state.paused = true
    activeDownloadsState[url] = state
    activeDownloadsMux.Unlock()
    log.Printf("Download paused: %s", url)
}

// markDownloadResumed quita la marca de pausa
func markDownloadResumed(url string) {
    activeDownloadsMux.Lock()
    state := activeDownloadsState[url]
    state.paused = false
    activeDownloadsState[url] = state
    activeDownloadsMux.Unlock()
    log.Printf("Download resumed: %s", url)
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
        if (n > 0) {
            totalBytes += n
            hash.Write(buf[:n])
        }
        
        if (err == io.EOF) {
            break
        }
        
        if (err != nil) {
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
    if (err != nil) {
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
        if (err != nil) {
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
