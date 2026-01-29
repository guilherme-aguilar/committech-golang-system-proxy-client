package main

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/hashicorp/yamux"
)

// --- CONFIGURAÇÕES ---

const (
	CertDir    = "certs"
	CaFile     = "certs/ca.crt"
	ClientCert = "certs/client.crt"
	ClientKey  = "certs/client.key"
)

type Config struct {
	ServerIP       string `toml:"server_ip"`
	Token          string `toml:"token"`
	Name           string `toml:"name"`
	ControlPort    string `toml:"control_port"`
	EnrollPort     string `toml:"enroll_port"`
	ReconnectDelay int    `toml:"reconnect_delay"`
}

func main() {
	// 1. Configuração
	flagPath := flag.String("config", "client.toml", "Caminho do arquivo TOML")
	flagIP := flag.String("server", "", "Sobrescreve Server IP")
	flagToken := flag.String("token", "", "Sobrescreve Token")
	flagName := flag.String("name", "", "Sobrescreve Nome")
	flag.Parse()

	cfg := loadConfig(*flagPath)

	// Overrides
	if *flagIP != "" {
		cfg.ServerIP = *flagIP
	}
	if *flagToken != "" {
		cfg.Token = *flagToken
	}
	if *flagName != "" {
		cfg.Name = *flagName
	}

	if cfg.Name == "" {
		host, _ := os.Hostname()
		cfg.Name = "proxy-" + host
	}

	if cfg.ServerIP == "" || cfg.Token == "" {
		log.Fatal("❌ Erro: 'server_ip' e 'token' são obrigatórios.")
	}

	log.Printf("[Init] Iniciando Agente: %s -> %s", cfg.Name, cfg.ServerIP)

	// Cria pasta certs se não existir
	ensureCertDir()

	// 2. BOOTSTRAP: Garantir CA (Loop de Retry)
	for {
		if err := ensureCACertificate(cfg.ServerIP, cfg.EnrollPort); err != nil {
			log.Printf("[Boot] ⚠️  Falha ao baixar CA: %v. Tentando em 5s...", err)
			time.Sleep(5 * time.Second)
			continue
		}
		break
	}

	// 3. AUTENTICAÇÃO: Carregar do disco OU Matricular (Loop de Retry)
	var clientIdentity tls.Certificate
	var err error

	for {
		clientIdentity, err = loadOrEnroll(cfg)
		if err != nil {
			log.Printf("[Auth] ⚠️  Falha na autenticação: %v. Tentando em 5s...", err)
			time.Sleep(5 * time.Second)
			continue
		}
		break
	}

	// 4. CONEXÃO: Túnel (Loop Infinito)
	tunnelAddr := fmt.Sprintf("%s%s", cfg.ServerIP, cfg.ControlPort)

	for {
		log.Printf("[Tunnel] 🔌 Conectando a %s...", tunnelAddr)
		err := connectTunnel(tunnelAddr, clientIdentity)

		log.Printf("[Tunnel] ❌ Desconectado: %v", err)
		log.Printf("[Tunnel] ⏳ Reconectando em %ds...", cfg.ReconnectDelay)
		time.Sleep(time.Duration(cfg.ReconnectDelay) * time.Second)
	}
}

// --- FUNÇÕES DE ARQUIVO E CERTIFICADO ---

func ensureCertDir() {
	if _, err := os.Stat(CertDir); os.IsNotExist(err) {
		os.MkdirAll(CertDir, 0755)
	}
}

func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}

// ensureCACertificate baixa o CA público se ele não existir
func ensureCACertificate(serverIP, port string) error {
	if fileExists(CaFile) {
		return nil
	}

	url := fmt.Sprintf("https://%s%s/ca.crt", serverIP, port)
	log.Printf("[Boot] 📥 Baixando CA de %s...", url)

	// Cliente inseguro APENAS para baixar a CA pública (Bootstrap)
	tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	client := &http.Client{Transport: tr, Timeout: 10 * time.Second}

	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("status HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	return os.WriteFile(CaFile, data, 0644)
}

// loadOrEnroll tenta ler do disco. Se falhar, pede novo para o servidor.
func loadOrEnroll(cfg Config) (tls.Certificate, error) {
	// A) Tenta carregar do disco
	if fileExists(ClientCert) && fileExists(ClientKey) {
		cert, err := tls.LoadX509KeyPair(ClientCert, ClientKey)
		if err == nil {
			// Verifica validade
			x509Cert, _ := x509.ParseCertificate(cert.Certificate[0])
			if time.Now().Before(x509Cert.NotAfter) {
				log.Println("[Auth] ✅ Identidade válida carregada do disco.")
				return cert, nil
			}
			log.Println("[Auth] ⚠️  Certificado expirou. Renovando...")
		} else {
			log.Printf("[Auth] ⚠️  Certificado corrompido: %v", err)
		}
	}

	// B) Matrícula (Enroll)
	log.Println("[Enroll] 📝 Solicitando novo certificado...")
	enrollURL := fmt.Sprintf("https://%s%s/enroll", cfg.ServerIP, cfg.EnrollPort)

	caPool, err := loadLocalCA()
	if err != nil {
		return tls.Certificate{}, err
	}

	// Configura cliente com a CA que acabamos de baixar
	client := &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: caPool}},
		Timeout:   10 * time.Second,
	}

	payload := map[string]string{"token": cfg.Token, "name": cfg.Name}
	jsonData, _ := json.Marshal(payload)

	resp, err := client.Post(enrollURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return tls.Certificate{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return tls.Certificate{}, fmt.Errorf("erro servidor: %s", body)
	}

	var res struct{ Cert, Key string }
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return tls.Certificate{}, err
	}

	// C) Salva no disco
	os.WriteFile(ClientCert, []byte(res.Cert), 0644)
	os.WriteFile(ClientKey, []byte(res.Key), 0600)
	log.Println("[Enroll] 💾 Certificados salvos com sucesso!")

	return tls.X509KeyPair([]byte(res.Cert), []byte(res.Key))
}

func loadLocalCA() (*x509.CertPool, error) {
	data, err := os.ReadFile(CaFile)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		return nil, fmt.Errorf("falha ao ler CA")
	}
	return pool, nil
}

// --- LÓGICA DO TÚNEL ---

func connectTunnel(addr string, cert tls.Certificate) error {
	caPool, err := loadLocalCA()
	if err != nil {
		return err
	}

	// Conecta mTLS
	conn, err := tls.Dial("tcp", addr, &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caPool,
	})
	if err != nil {
		return err
	}

	// Inicia Yamux Server (Client side age como Server no multiplexing)
	session, err := yamux.Server(conn, nil)
	if err != nil {
		conn.Close()
		return err
	}

	log.Println("[Tunnel] 🔒 Conexão Segura Estabelecida! Aguardando comandos...")

	for {
		stream, err := session.Accept()
		if err != nil {
			return err
		}
		go handleStream(stream)
	}
}

func handleStream(stream net.Conn) {
	defer stream.Close()

	// Timeout para ler o cabeçalho inicial do pedido
	stream.SetReadDeadline(time.Now().Add(10 * time.Second))
	br := bufio.NewReader(stream)
	req, err := http.ReadRequest(br)
	stream.SetReadDeadline(time.Time{}) // Remove timeout para o resto da conexão

	if err != nil {
		return
	}

	log.Printf("[Traffic] %s %s", req.Method, req.Host)

	if req.Method == http.MethodConnect {
		handleHTTPS(stream, req)
	} else {
		handleHTTP(stream, req)
	}
}

func handleHTTPS(stream net.Conn, req *http.Request) {
	dest, err := net.DialTimeout("tcp", req.Host, 10*time.Second)
	if err != nil {
		return
	}
	defer dest.Close()

	stream.Write([]byte("HTTP/1.1 200 OK\r\n\r\n"))

	// Proxy Bidirecional
	errChan := make(chan error, 2)
	go func() { _, err := io.Copy(dest, stream); errChan <- err }()
	go func() { _, err := io.Copy(stream, dest); errChan <- err }()
	<-errChan
}

func handleHTTP(stream net.Conn, req *http.Request) {
	client := &http.Client{
		// Não segue redirects (devolve pro navegador resolver)
		CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse },
		Timeout:       30 * time.Second,
	}

	// Limpa URI para evitar erro do Go Client
	req.RequestURI = ""
	req.URL.Scheme = "http"
	req.URL.Host = req.Host

	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	resp.Write(stream)
}

func loadConfig(path string) Config {
	cfg := Config{
		ControlPort:    ":8081",
		EnrollPort:     ":8082",
		ReconnectDelay: 5,
	}
	if fileExists(path) {
		toml.DecodeFile(path, &cfg)
	}
	return cfg
}
