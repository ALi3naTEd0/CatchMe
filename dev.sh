#!/bin/bash

# Colores para mejor visibilidad
GREEN='\033[0;32m'
BLUE='\033[0;34m'
RED='\033[0;31m'
NC='\033[0m'

# Array para guardar PIDs
declare -a PIDS=()

# Definir directorio base
BASE_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
SERVER_DIR="$BASE_DIR/server"
APP_DIR="$BASE_DIR/app"

# Matar proceso que use el puerto 8080
kill_port() {
    pid=$(lsof -t -i:8080)
    if [ ! -z "$pid" ]; then
        echo -e "${RED}Matando proceso existente en puerto 8080 (PID: $pid)${NC}"
        kill -9 $pid
    fi
}

# Función para matar procesos al salir
cleanup() {
    echo -e "\n${BLUE}Deteniendo servicios...${NC}"
    # Matar todo el árbol de procesos
    for pid in "${PIDS[@]}"; do
        pkill -P $pid
        kill -9 $pid 2>/dev/null
    done
    echo -e "${GREEN}Servicios detenidos${NC}"
    exit 0
}

# Atrapar señales de terminación
trap cleanup SIGINT SIGTERM EXIT

# Limpiar puerto antes de iniciar
kill_port

# Iniciar servidor Go
echo -e "${GREEN}Iniciando servidor Go...${NC}"
cd "$SERVER_DIR" && go run main.go &
PIDS+=($!)

# Iniciar Flutter
echo -e "${GREEN}Iniciando Flutter...${NC}"
cd "$APP_DIR" && flutter run -d linux &
PIDS+=($!)

echo -e "${BLUE}PIDs: ${PIDS[@]}${NC}"

# Esperar a que cualquier proceso termine
wait -n

# Si algún proceso termina, limpiar todo
echo -e "${RED}Un proceso ha terminado, cerrando todos los servicios...${NC}"
cleanup
