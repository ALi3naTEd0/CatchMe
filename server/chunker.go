package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
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
	completedChunks := 0
	activeChunks := 0

	for _, chunk := range d.Chunks {
		chunk.mu.Lock()
		chunkSize := chunk.End - chunk.Start + 1

		if chunk.Status == ChunkCompleted {
			downloaded += chunkSize
			completedChunks++
		} else if chunk.Status == ChunkActive && chunk.Progress > 0 {
			// More precise completion detection
			remaining := chunkSize - chunk.Progress
			if remaining <= 32 || chunk.Progress >= chunkSize {
				downloaded += chunkSize
				completedChunks++
			} else {
				downloaded += chunk.Progress
				activeChunks++
			}
		}
		chunk.mu.Unlock()
	}

	// Only force to total size if we're really at the end
	if activeChunks == 0 && total-downloaded <= 1024 {
		downloaded = total - 1 // Leave 1 byte to prevent hitting 100.1%
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
