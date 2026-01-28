#!/bin/bash
# Arquivo: scripts/install_linux.sh

APP_NAME="proxy-client"
INSTALL_DIR="/opt/proxy-client"
SERVICE_NAME="proxy-client"

if [ "$EUID" -ne 0 ]; then echo "Rode como root"; exit 1; fi

echo "Instalando Client..."
systemctl stop $SERVICE_NAME &>/dev/null

mkdir -p $INSTALL_DIR
cp $APP_NAME $INSTALL_DIR/
chmod +x $INSTALL_DIR/$APP_NAME

# Cria serviço (Ajuste os argumentos se precisar passar IP via flag)
cat <<EOF > /etc/systemd/system/$SERVICE_NAME.service
[Unit]
Description=Proxy Client Service
After=network.target

[Service]
ExecStart=$INSTALL_DIR/$APP_NAME
Restart=always
WorkingDirectory=$INSTALL_DIR
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable $SERVICE_NAME
systemctl start $SERVICE_NAME

echo "✅ Client rodando!"