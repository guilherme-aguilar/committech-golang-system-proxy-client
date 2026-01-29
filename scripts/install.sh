#!/bin/bash

# Cores
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

APP_NAME="proxy-client"
SERVICE_NAME="proxy-client"
INSTALL_DIR="/opt/proxy-client"
CERT_DIR="$INSTALL_DIR/certs"
USER="proxyclient"

echo -e "${BLUE}>>> Instalação do Proxy Client...${NC}"

# 1. Root Check
if [ "$EUID" -ne 0 ]; then 
    echo -e "${RED}Erro: Rode como root.${NC}"
    exit 1
fi

# 2. Validação do Pacote (Confirma se o binário está na pasta atual)
if [[ ! -f "$APP_NAME" ]]; then
    echo -e "${RED}Erro: Binário '$APP_NAME' não encontrado na pasta atual.${NC}"
    ls -l
    exit 1
fi

# 3. Parar serviço antigo
systemctl stop $SERVICE_NAME &>/dev/null

# 4. Criar usuário
if ! id "$USER" &>/dev/null; then 
    echo "👤 Criando usuário de serviço: $USER"
    useradd -r -s /bin/false $USER
fi

# 5. Estrutura de Pastas
mkdir -p $INSTALL_DIR
mkdir -p $CERT_DIR

# 6. Copiar Binário
echo "📦 Atualizando binário..."
cp -f "$APP_NAME" "$INSTALL_DIR/"

# 7. Preservação de Configuração
if [ -f "$INSTALL_DIR/client.toml" ]; then
    echo -e "${YELLOW}⚙️  Configuração existente preservada.${NC}"
    cp "client.toml" "$INSTALL_DIR/client.toml.new"
else
    echo -e "${GREEN}⚙️  Instalando configuração padrão.${NC}"
    cp "client.toml" "$INSTALL_DIR/"
fi

# 8. Permissões
chown -R $USER:$USER $INSTALL_DIR
chmod +x "$INSTALL_DIR/$APP_NAME"
chmod 700 "$CERT_DIR"

# 9. SystemD
echo "🔧 Configurando serviço..."
cat <<EOF > /etc/systemd/system/$SERVICE_NAME.service
[Unit]
Description=Proxy Manager Client Agent
After=network.target network-online.target
Wants=network-online.target

[Service]
User=$USER
Group=$USER
WorkingDirectory=$INSTALL_DIR
ExecStart=$INSTALL_DIR/$APP_NAME
Restart=always
RestartSec=10
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable $SERVICE_NAME >/dev/null
systemctl start $SERVICE_NAME

sleep 2

if systemctl is-active --quiet $SERVICE_NAME; then
    echo -e "${GREEN}✅ INSTALAÇÃO CONCLUÍDA!${NC}"
    echo "Edite o token em: nano $INSTALL_DIR/client.toml"
    echo "Reinicie: systemctl restart $SERVICE_NAME"
else
    echo -e "${RED}❌ Erro ao iniciar serviço. Verifique 'journalctl -u $SERVICE_NAME'.${NC}"
    exit 1
fi