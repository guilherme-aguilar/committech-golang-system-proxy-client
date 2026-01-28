#!/bin/bash
# Arquivo: release.sh

VERSION=$1
if [ -z "$VERSION" ]; then
    echo "❌ Uso: ./release.sh v1.0"
    exit 1
fi

APP_NAME="proxy-client"
DIST_DIR="dist"

echo "🧹 Limpando..."
rm -rf $DIST_DIR
mkdir -p $DIST_DIR

# ---------------------------------------------------------
# 1. BUILD PARA LINUX (Servidores, VPS, RPi)
# ---------------------------------------------------------
echo "🐧 Compilando para Linux (AMD64)..."
env CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o $DIST_DIR/linux/$APP_NAME ./cmd/client

# Copia script de instalação
cp scripts/install_linux.sh $DIST_DIR/linux/install.sh

# Empacota
cd $DIST_DIR/linux
tar -czvf ../$APP_NAME-linux-$VERSION.tar.gz *
cd ../..

# ---------------------------------------------------------
# 2. BUILD PARA WINDOWS (Usuários Comuns)
# ---------------------------------------------------------
echo "🪟 Compilando para Windows (AMD64)..."
env CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o $DIST_DIR/windows/$APP_NAME.exe ./cmd/client

# Empacota (Zip é melhor para Windows)
cd $DIST_DIR/windows
zip -r ../$APP_NAME-windows-$VERSION.zip *
cd ../..

# ---------------------------------------------------------
# 3. GIT TAG E UPLOAD
# ---------------------------------------------------------
echo "🏷️  Criando Tag Git: $VERSION..."
git add .
git commit -m "Release $VERSION"
git tag -a "$VERSION" -m "Client Release $VERSION"
git push origin "$VERSION"

echo ""
echo "✅ SUCESSO! Arquivos gerados em 'dist/':"
echo "   📄 Linux:   $APP_NAME-linux-$VERSION.tar.gz"
echo "   📄 Windows: $APP_NAME-windows-$VERSION.zip"
echo ""
echo "➡️  Vá no GitHub Releases e faça o upload desses dois arquivos!"