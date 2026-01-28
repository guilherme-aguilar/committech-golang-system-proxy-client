#!/bin/bash
# Arquivo: setup.sh do Client

REPO="guilherme-aguilar/committech-golang-system-proxy-client
FILE_PREFIX="proxy-client-linux"

echo "🔍 Buscando última versão..."
LATEST=$(curl -s "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')

if [ -z "$LATEST" ]; then echo "Erro: Nenhuma release encontrada"; exit 1; fi

URL="https://github.com/$REPO/releases/download/$LATEST/$FILE_PREFIX-$LATEST.tar.gz"
TMP=$(mktemp -d)

echo "⬇️  Baixando $LATEST..."
curl -sL -o "$TMP/client.tar.gz" "$URL"

tar -xzf "$TMP/client.tar.gz" -C "$TMP"
cd "$TMP"
bash install.sh

rm -rf "$TMP"