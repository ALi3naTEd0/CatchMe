package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

// ServiceManager gestiona la ejecución como un servicio
type ServiceManager struct {
	isRunning      bool
	shutdownSignal chan os.Signal
	httpPort       int
	logFile        *os.File
}

// NewServiceManager crea un nuevo gestor de servicios
func NewServiceManager(httpPort int) *ServiceManager {
	return &ServiceManager{
		shutdownSignal: make(chan os.Signal, 1),
		httpPort:       httpPort,
	}
}

// Setup inicializa el servicio
func (sm *ServiceManager) Setup() error {
	// Configurar manejo de señales
	signal.Notify(sm.shutdownSignal, syscall.SIGINT, syscall.SIGTERM)

	// Crear directorios necesarios
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("error getting home directory: %v", err)
	}

	// Crear estructura de directorios
	dirs := []string{
		filepath.Join(homeDir, ".catchme"),
		filepath.Join(homeDir, ".catchme", "logs"),
		filepath.Join(homeDir, ".catchme", "downloads"),
		filepath.Join(homeDir, ".catchme", "temp"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("error creating directory %s: %v", dir, err)
		}
	}

	// Configurar logging
	logPath := filepath.Join(homeDir, ".catchme", "logs", "service.log")
	sm.logFile, err = os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("error opening log file: %v", err)
	}

	log.SetOutput(sm.logFile)
	log.Println("CatchMe service initialized")

	return nil
}

// Start inicia el servicio
func (sm *ServiceManager) Start() error {
	if sm.isRunning {
		return fmt.Errorf("service already running")
	}

	// Iniciar el servidor HTTP en segundo plano
	go func() {
		if err := startHTTPServer(); err != nil {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	// Iniciar WebSocket en segundo plano
	go func() {
		if err := startWebSocketServer(); err != nil {
			log.Printf("WebSocket server error: %v", err)
		}
	}()

	sm.isRunning = true
	log.Printf("CatchMe service started - HTTP on port %d, WebSocket enabled", sm.httpPort)

	// Esperar señal de apagado
	go func() {
		sig := <-sm.shutdownSignal
		log.Printf("Received signal: %v", sig)
		sm.Stop()
	}()

	return nil
}

// Stop detiene el servicio
func (sm *ServiceManager) Stop() {
	if !sm.isRunning {
		return
	}

	log.Println("Stopping CatchMe service...")

	// Cerrar conexiones activas y detener servidores
	stopHTTPServer()
	stopWebSocketServer()

	// Limpiar recursos temporales
	cleanupTemporaryFiles()

	sm.isRunning = false
	if sm.logFile != nil {
		sm.logFile.Close()
	}

	log.Println("CatchMe service stopped")
}

// IsRunning devuelve si el servicio está en ejecución
func (sm *ServiceManager) IsRunning() bool {
	return sm.isRunning
}

// Funciones auxiliares para el servidor
// Remover el parámetro port no utilizado
func startHTTPServer() error {
	// Implementación del servidor HTTP
	return nil
}

func stopHTTPServer() {
	// Detener servidor HTTP
}

func startWebSocketServer() error {
	// Implementación del servidor WebSocket
	return nil
}

func stopWebSocketServer() {
	// Detener servidor WebSocket
}

func cleanupTemporaryFiles() {
	// Limpiar archivos temporales que ya no son necesarios
}

// RunAsService ejecuta la aplicación como un servicio
func RunAsService(httpPort int) error {
	service := NewServiceManager(httpPort)

	if err := service.Setup(); err != nil {
		return fmt.Errorf("service setup failed: %v", err)
	}

	if err := service.Start(); err != nil {
		return fmt.Errorf("service start failed: %v", err)
	}

	// Mantenerse en ejecución hasta recibir señal
	select {}
}
