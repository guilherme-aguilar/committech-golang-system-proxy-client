#!/bin/bash
# Instalação One-Line

REPO="guilherme-aguilar/committech-golang-system-proxy-client"
PROJECT="proxy-client"
OS="linux"
ARCH="amd64"

echo -e "\033[0;34m>>> Iniciando Instalador do Client...\033[0m"

# 1. Cria diretório temporário seguro
TMP_DIR=$(mktemp -d)
echo "📂 Trabalhando em: $TMP_DIR"
cd "$TMP_DIR" || exit 1

# 2. Busca URL
echo "🔍 Buscando versão mais recente..."
LATEST_URL=$(curl -s "https://api.github.com/repos/$REPO/releases/latest" \
    | grep "browser_download_url" \
    | grep "$PROJECT-$OS" \
    | cut -d '"' -f 4)

if [ -z "$LATEST_URL" ]; then
    echo -e "\033[0;31m❌ Erro: Release não encontrada.\033[0m"
    exit 1
fi

FILENAME=$(basename "$LATEST_URL")

# 3. Download
echo "⬇️  Baixando $FILENAME..."
curl -L -o "$FILENAME" "$LATEST_URL" --fail

# 4. Extração
echo "📦 Extraindo..."
tar -xzf "$FILENAME"

# Verifica se extraiu uma pasta (padrão do seu release.sh)
if [ -d "$PROJECT" ]; then
    cd "$PROJECT"
fi

# 5. Instalação
echo "🚀 Executando install.sh..."
chmod +x install.sh
./install.sh

# 6. Limpeza
cd /
rm -rf "$TMP_DIR"
echo -e "\033[0;32m✅ Limpeza de arquivos temporários concluída.\033[0m"