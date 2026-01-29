#!/bin/bash
# Uso: curl -sL https://seu-repo/setup.sh | bash

REPO="guilherme-aguilar/committech-golang-system-proxy-client" 
PROJECT="proxy-client"

echo ">>> Iniciando Instalador do Client..."

# Detecta arquitetura (simples, assume amd64 linux por enquanto baseado no seu release)
OS="linux"
ARCH="amd64"

# Busca última release via API do GitHub
echo "🔍 Buscando versão mais recente..."
LATEST_URL=$(curl -s "https://api.github.com/repos/$REPO/releases/latest" | grep "browser_download_url" | grep "$PROJECT-$OS" | cut -d '"' -f 4)

if [ -z "$LATEST_URL" ]; then
    echo "Erro: Não foi possível encontrar a release."
    exit 1
fi

FILENAME=$(basename "$LATEST_URL")

echo "⬇️  Baixando $FILENAME..."
wget -q --show-progress "$LATEST_URL"

echo "📦 Extraindo..."
tar -xzf "$FILENAME"
cd "$PROJECT"

echo "🚀 Executando instalação..."
chmod +x install.sh
sudo ./install.sh

# Limpeza
cd ..
rm -rf "$PROJECT" "$FILENAME"