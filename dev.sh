#!/bin/bash

# ...existing color and initialization code...

# Compilar servidor Go
echo -e "${GREEN}Compilando servidor Go...${NC}"
cd server && go build -o ../assets/server/catchme
cd ..

# Iniciar Flutter
echo -e "${GREEN}Iniciando Flutter...${NC}"
flutter run -d linux

