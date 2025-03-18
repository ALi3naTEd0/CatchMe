#!/bin/bash
set -e

echo "Building CatchMe server..."
go build -o catchme-server .

echo "Server built successfully!"
echo "Run with ./catchme-server"
