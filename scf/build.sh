#!/bin/bash
# Build script for Tencent Cloud SCF

set -e

cd "$(dirname "$0")"

echo "Building for SCF..."
GOOS=linux GOARCH=amd64 go build -o main main.go

echo "Creating zip..."
zip main.zip main

echo "Done! Upload main.zip to Tencent Cloud SCF"
echo ""
echo "Or deploy with Serverless CLI:"
echo "  serverless deploy"
