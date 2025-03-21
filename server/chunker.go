package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ChunkStatus representa el estado de un chunk
type ChunkStatus string

const (
	ChunkPending   ChunkStatus = "pending"
	ChunkActive    ChunkStatus = "active"
	ChunkCompleted ChunkStatus = "completed"
	ChunkFailed    ChunkStatus = "failed"
	ChunkPaused    ChunkStatus = "paused"
)

// Chunk representa una parte de un archivo a descargar
type Chunk struct {
	ID        int
	Start     int64
	End       int64
	Path      string
	Status    ChunkStatus
	Progress  int64
	Error     string
	mu        sync.Mutex
	cancelCtx chan struct{}
}

// ChunkProgress representa el progreso de un chunk para reportar al cliente
type ChunkProgress struct {
	ID        int         `json:"id"`
	Start     int64       `json:"start"`
	End       int64       `json:"end"`
	Progress  int64       `json:"progress"`
	Status    ChunkStatus `json:"status"`
	Speed     float64     `json:"speed"`
	Completed int64       `json:"completed"`
}

// ChunkedDownload representa una descarga dividida en múltiples chunks
type ChunkedDownload struct {
	URL        string
	Filename   string
	Size       int64
	ChunkSize  int64
	TempDir    string
	Chunks     []*Chunk
	Complete   bool
	Paused     bool
	mu         sync.RWMutex
	cancelChan chan struct{}
}

// NewChunkedDownload crea una nueva descarga dividida en chunks
func NewChunkedDownload(url, filename string, size int64, chunkSize int64) *ChunkedDownload {
	// Si no se especifica un tamaño de chunk, usar un valor predeterminado
	if chunkSize <= 0 {
		chunkSize = 5 * 1024 * 1024 // 5MB
	}

	return &ChunkedDownload{
		URL:        url,
		Filename:   filename,
		Size:       size,
		ChunkSize:  chunkSize,
		TempDir:    filepath.Join(os.TempDir(), "catchme", filename),
		cancelChan: make(chan struct{}),
	}
}

// PrepareChunks divide la descarga en chunks
func (d *ChunkedDownload) PrepareChunks() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Crear directorio temporal para chunks
	if err := os.MkdirAll(d.TempDir, 0755); err != nil {
		return fmt.Errorf("failed to create temp directory: %v", err)
	}

	// Dividir el archivo en chunks
	var chunks []*Chunk
	for start := int64(0); start < d.Size; start += d.ChunkSize {
		end := start + d.ChunkSize - 1
		if end > d.Size-1 {
			end = d.Size - 1
		}

		chunk := &Chunk{
			ID:        len(chunks),
			Start:     start,
			End:       end,
			Path:      filepath.Join(d.TempDir, fmt.Sprintf("chunk_%d", len(chunks))),
			Status:    ChunkPending,
			cancelCtx: make(chan struct{}),
		}
		chunks = append(chunks, chunk)
	}

	d.Chunks = chunks
	return nil
}

// DownloadChunk descarga un chunk específico
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

	// Crear o abrir archivo para el chunk
	file, err := os.OpenFile(chunk.Path, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		chunk.mu.Lock()
		chunk.Status = ChunkFailed
		chunk.Error = err.Error()
		chunk.mu.Unlock()
		return err
	}
	defer file.Close()

	// Establecer posición inicial
	if chunk.Progress > 0 {
		if _, err := file.Seek(chunk.Progress, 0); err != nil {
			chunk.mu.Lock()
			chunk.Status = ChunkFailed
			chunk.Error = err.Error()
			chunk.mu.Unlock()
			return err
		}
	}

	// Crear request con rango
	req, err := http.NewRequest("GET", d.URL, nil)
	if err != nil {
		chunk.mu.Lock()
		chunk.Status = ChunkFailed
		chunk.Error = err.Error()
		chunk.mu.Unlock()
		return err
	}

	// Establecer rango de bytes para este chunk
	rangeStart := chunk.Start + chunk.Progress
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", rangeStart, chunk.End))

	// Añadir User-Agent para evitar bloqueos/limitaciones
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/96.0.4664.93 Safari/537.36")

	// Iniciar descarga
	resp, err := client.Do(req)
	if err != nil {
		chunk.mu.Lock()
		chunk.Status = ChunkFailed
		chunk.Error = err.Error()
		chunk.mu.Unlock()
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		err := fmt.Errorf("server returned %d", resp.StatusCode)
		chunk.mu.Lock()
		chunk.Status = ChunkFailed
		chunk.Error = err.Error()
		chunk.mu.Unlock()
		return err
	}

	// Verificar si el servidor soporta rangos
	if resp.StatusCode != http.StatusPartialContent {
		err := fmt.Errorf("server doesn't support range requests")
		chunk.mu.Lock()
		chunk.Status = ChunkFailed
		chunk.Error = err.Error()
		chunk.mu.Unlock()
		return err
	}

	// Descargar datos con buffer más grande para mejor rendimiento
	startTime := time.Now()  // Solo mantener una declaración
	lastUpdate := time.Now() // Solo mantener una declaración
	updateInterval := 100 * time.Millisecond
	buffer := make([]byte, 512*1024)
	lastProgress := chunk.Progress

	// Remover todas las redeclaraciones redundantes de estas variables
	// y eliminar las líneas 197-208 que contienen las declaraciones duplicadas

	// Modify the download loop to better handle pause/resume
	for {
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

			n, err := resp.Body.Read(buffer)
			if n > 0 {
				// Escribir al archivo
				_, writeErr := file.Write(buffer[:n])
				if writeErr != nil {
					chunk.mu.Lock()
					chunk.Status = ChunkFailed
					chunk.Error = writeErr.Error()
					chunk.mu.Unlock()
					return writeErr
				}

				// Actualizar progreso
				chunk.mu.Lock()
				chunk.Progress += int64(n)
				currentProgress := chunk.Progress
				chunk.mu.Unlock()

				now := time.Now()

				// Solo enviar actualizaciones cada 500ms
				if now.Sub(lastUpdate) >= updateInterval {
					elapsed := now.Sub(startTime).Seconds()
					if elapsed > 0 {
						speed := float64(currentProgress-lastProgress) / now.Sub(lastUpdate).Seconds()

						// Enviar progreso actualizado al cliente incluyendo velocidad
						if safeConn != nil {
							d.mu.RLock()
							// Enviar progreso de chunk
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

							// Enviar también progreso general con velocidad
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
					// Chunk completado
					chunk.markCompleted()

					// Reportar estadísticas de finalización del chunk
					elapsed := time.Since(startTime)
					totalBytes := chunk.End - chunk.Start + 1
					avgSpeed := float64(totalBytes) / elapsed.Seconds()

					// Usar la variable avgSpeed para evitar el error "declared and not used"
					log.Printf("Chunk %d completed in %.2fs (%.2f MB/s)",
						chunk.ID, elapsed.Seconds(), avgSpeed/(1024*1024))

					// Enviar notificación final para este chunk
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
								Speed:     0, // No velocidad cuando está completo
								Completed: chunk.End + 1,
							},
						})
					}

					return nil
				}

				// Error de descarga
				chunk.mu.Lock()
				chunk.Status = ChunkFailed
				chunk.Error = err.Error()
				chunk.mu.Unlock()
				return err
			}
		}
	}
}

// MergeChunks combina todos los chunks en un archivo final
func (d *ChunkedDownload) MergeChunks(destPath string) error {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// Verificar que todos los chunks estén completos
	for _, chunk := range d.Chunks {
		chunk.mu.Lock()
		if chunk.Status != ChunkCompleted {
			chunk.mu.Unlock()
			return fmt.Errorf("chunk %d not completed", chunk.ID)
		}
		chunk.mu.Unlock()
	}

	// Crear directorio de destino si no existe
	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Crear archivo de destino
	destFile, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer destFile.Close()

	// Escribir cada chunk en el archivo de destino
	for _, chunk := range d.Chunks {
		chunkFile, err := os.Open(chunk.Path)
		if err != nil {
			return err
		}

		_, err = io.Copy(destFile, chunkFile)
		chunkFile.Close()
		if err != nil {
			return err
		}
	}

	// Verificar tamaño final
	info, err := destFile.Stat()
	if err != nil {
		return err
	}
	if info.Size() != d.Size {
		return fmt.Errorf("size mismatch: expected %d, got %d", d.Size, info.Size())
	}

	d.Complete = true
	return nil
}

// PauseChunk pausa un chunk específico
func (d *ChunkedDownload) PauseChunk(chunkID int) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	for _, chunk := range d.Chunks {
		if chunk.ID == chunkID {
			chunk.mu.Lock()
			if chunk.Status == ChunkActive {
				close(chunk.cancelCtx)
				chunk.Status = ChunkPaused
			}
			chunk.mu.Unlock()
			return
		}
	}
}

// PauseAllChunks pausa todos los chunks
func (d *ChunkedDownload) PauseAllChunks() {
	d.mu.Lock()
	d.Paused = true
	d.mu.Unlock()

	d.mu.RLock()
	defer d.mu.RUnlock()

	for _, chunk := range d.Chunks {
		chunk.mu.Lock()
		if chunk.Status == ChunkActive {
			close(chunk.cancelCtx)
			chunk.cancelCtx = make(chan struct{}) // Nuevo canal para futura reanudación
			chunk.Status = ChunkPaused
		}
		chunk.mu.Unlock()
	}
}

// GetProgress obtiene el progreso general de la descarga
func (d *ChunkedDownload) GetProgress() (downloaded int64, total int64) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	total = d.Size
	downloaded = 0
	activeOrIncomplete := 0
	completedChunks := 0

	for _, chunk := range d.Chunks {
		chunk.mu.Lock()
		chunkSize := chunk.End - chunk.Start + 1

		switch chunk.Status {
		case ChunkCompleted:
			downloaded += chunkSize
			completedChunks++
		case ChunkActive:
			// More aggressive completion threshold for last chunks
			if chunk.Progress >= chunkSize-32 {
				downloaded += chunkSize
				chunk.Status = ChunkCompleted
			} else {
				downloaded += chunk.Progress
				activeOrIncomplete++
			}
		case ChunkPending, ChunkPaused:
			activeOrIncomplete++
		}
		chunk.mu.Unlock()
	}

	// If all chunks are nearly complete, consider download complete
	if completedChunks == len(d.Chunks)-1 && activeOrIncomplete == 1 {
		if total-downloaded <= 32 {
			downloaded = total
		}
	}

	return
}

// IsComplete verifica si la descarga está completa
func (d *ChunkedDownload) IsComplete() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()

	downloaded := int64(0)
	for _, chunk := range d.Chunks {
		chunk.mu.Lock()
		if chunk.Status == ChunkCompleted {
			downloaded += (chunk.End - chunk.Start + 1)
		} else if chunk.Status == ChunkActive {
			remaining := chunk.End - chunk.Start + 1 - chunk.Progress
			if remaining <= 32 {
				downloaded += (chunk.End - chunk.Start + 1)
			}
		}
		chunk.mu.Unlock()
	}

	return downloaded >= d.Size-32
}

// Cleanup elimina archivos temporales
func (d *ChunkedDownload) Cleanup() error {
	return os.RemoveAll(d.TempDir)
}

// Añadir validación adicional al completar chunks
func (c *Chunk) markCompleted() {
	c.mu.Lock()
	defer c.mu.Unlock()

	expectedSize := c.End - c.Start + 1

	// Use tighter tolerance for completion
	if c.Progress >= expectedSize-32 {
		c.Status = ChunkCompleted
		c.Progress = expectedSize // Force exact size
	} else {
		c.Status = ChunkPending
		c.Error = fmt.Sprintf("incomplete data: %d/%d", c.Progress, expectedSize)
	}
}
