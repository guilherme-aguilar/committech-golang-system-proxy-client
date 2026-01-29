#!/bin/bash
# Instalação One-Line: curl -sL https://raw.github.../setup.sh | bash

# 1. Configurações do Repositório
REPO="guilherme-aguilar/committech-golang-system-proxy-client"
PROJECT="proxy-client"
OS="linux"
ARCH="amd64"

echo -e "\033[0;34m>>> Iniciando Instalador do Client...\033[0m"

# 2. Busca a URL da última release compatível (Linux/AMD64)
echo "🔍 Buscando versão mais recente no GitHub..."
LATEST_URL=$(curl -s "https://api.github.com/repos/$REPO/releases/latest" \
    | grep "browser_download_url" \
    | grep "$PROJECT-$OS" \
    | cut -d '"' -f 4)

if [ -z "$LATEST_URL" ]; then
    echo -e "\033[0;31m❌ Erro: Não foi possível encontrar a release.\033[0m"
    echo "Verifique se o repositório é Público ou se a Release foi criada corretamente."
    exit 1
fi

FILENAME=$(basename "$LATEST_URL")

# 3. Baixa o arquivo (Usando CURL, pois seu server não tem wget)
echo "⬇️  Baixando $FILENAME..."
curl -L -o "$FILENAME" "$LATEST_URL" --fail

if [ $? -ne 0 ]; then
    echo -e "\033[0;31m❌ Falha no download.\033[0m"
    exit 1
fi

# 4. Extração
echo "📦 Extraindo..."
# Remove pasta antiga se existir para evitar conflitos
rm -rf "$PROJECT"
tar -xzf "$FILENAME"

# Entra na pasta extraída (que o release.sh criou como 'proxy-client')
if [ ! -d "$PROJECT" ]; then
    echo -e "\033[0;31m❌ Erro: Pasta '$PROJECT' não encontrada após extração.\033[0m"
    exit 1
fi
cd "$PROJECT"

# 5. Instalação
echo "🚀 Executando script de instalação..."
chmod +x install.sh

# Roda direto (sem sudo, pois você já é root)
./install.sh

# 6. Limpeza (Remove o tar.gz e a pasta extraída após instalar)
cd ..
rm -rf "$PROJECT" "$FILENAME"