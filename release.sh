#!/bin/bash

# Cores
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m'

# Verifica GitHub CLI
if ! command -v gh &> /dev/null; then
    echo -e "${RED}Erro: GitHub CLI ('gh') não instalado.${NC}"
    exit 1
fi

VERSION=$1
if [ -z "$VERSION" ]; then
    echo -e "${RED}Erro: Informe a versão (ex: ./release.sh v1.0.0)${NC}"
    exit 1
fi

# Git Check (Opcional: pode comentar se quiser forçar release com git sujo)
if [[ -n $(git status -s) ]]; then
    echo -e "${RED}Erro: Git sujo. Faça commit antes.${NC}"
    exit 1
fi

BINARY_NAME="proxy-client"
DIST_DIR="dist/proxy-client"
ARCHIVE_NAME="proxy-client-linux-${VERSION}.tar.gz"

echo -e "${GREEN}>>> Iniciando Release do CLIENT: $VERSION${NC}"

# Limpeza
rm -rf dist
mkdir -p $DIST_DIR

echo "🔨 Compilando Client (Static)..."
# Compila o conteúdo de ./cmd
env CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o $DIST_DIR/$BINARY_NAME ./cmd

if [ $? -ne 0 ]; then
    echo -e "${RED}❌ Erro na compilação!${NC}"
    exit 1
fi

echo "📂 Copiando arquivos..."
cp client.toml $DIST_DIR/
# AQUI ESTAVA A DIFERENÇA: Usando scripts/install.sh
cp scripts/install.sh $DIST_DIR/install.sh 

echo "📦 Compactando..."
cd dist
tar -czvf $ARCHIVE_NAME proxy-client/
rm -rf proxy-client/
cd ..

FILE_TO_UPLOAD="dist/$ARCHIVE_NAME"

echo "🏷️  Git Tag..."
# Remove tag local se existir para evitar conflito
if git rev-parse "$VERSION" >/dev/null 2>&1; then
    git tag -d "$VERSION"
fi
git tag -a "$VERSION" -m "Client Release $VERSION"
git push origin "$VERSION" --force

echo "🚀 Subindo para o GitHub..."
gh release create "$VERSION" "$FILE_TO_UPLOAD" \
    --title "Client $VERSION" \
    --notes "Release automática do Client." \
    --latest

if [ $? -eq 0 ]; then
    echo -e "${GREEN}✅ SUCESSO! Cliente enviado.${NC}"
    rm -rf dist
else
    echo -e "${RED}❌ Erro no upload.${NC}"
fi