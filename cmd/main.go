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
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/hashicorp/yamux"
)

// --- CONSTANTES ---
const (
	CertDir    = "certs"
	CaFile     = "certs/ca.crt"
	ClientCert = "certs/client.crt"
	ClientKey  = "certs/client.key"
)

// --- CONFIGURAÇÃO ---
type Config struct {
	Agent  AgentConfig  `toml:"agent"`
	Server ServerConfig `toml:"server"`
}

type AgentConfig struct {
	ID    string `toml:"id"`
	Group string `toml:"group"`
	Token string `toml:"token"`
}

type ServerConfig struct {
	URL      string `toml:"url"`      // Ex: https://177.104...:8082
	Insecure bool   `toml:"insecure"` // Aceitar certificado auto-assinado
	Secret   string `toml:"secret"`   // Segredo da Blindagem (Novo!)
}

func main() {
	// 1. Carrega Configuração
	flagPath := flag.String("config", "client.toml", "Caminho do arquivo TOML")
	flag.Parse()

	cfg := loadConfig(*flagPath)

	// Validações Básicas
	if cfg.Server.URL == "" || cfg.Agent.Token == "" {
		log.Fatal("❌ Erro: 'server.url' e 'agent.token' são obrigatórios no client.toml.")
	}
	if cfg.Agent.ID == "" {
		host, _ := os.Hostname()
		cfg.Agent.ID = "proxy-" + host
	}

	log.Printf("[Init] Iniciando Agente: %s -> %s", cfg.Agent.ID, cfg.Server.URL)

	ensureCertDir()

	// 2. BOOTSTRAP: Garantir CA (Loop de Retry)
	for {
		if err := ensureCACertificate(cfg); err != nil {
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
	tunnelAddr := getTunnelAddress(cfg.Server.URL)

	for {
		log.Printf("[Tunnel] 🔌 Conectando a %s...", tunnelAddr)
		err := connectTunnel(tunnelAddr, clientIdentity, cfg)

		log.Printf("[Tunnel] ❌ Desconectado: %v", err)
		log.Printf("[Tunnel] ⏳ Reconectando em 5s...")
		time.Sleep(5 * time.Second)
	}
}

// --- FUNÇÕES AUXILIARES ---

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

// Garante formato correto da URL (sempre com https://)
func formatURL(base, path string) string {
	base = strings.TrimRight(base, "/")
	if !strings.HasPrefix(base, "http") {
		base = "https://" + base
	}
	return base + path
}

// Converte URL de Enroll (:8082) para Porta do Túnel (:8081)
func getTunnelAddress(enrollURL string) string {
	u, err := url.Parse(enrollURL)
	if err != nil {
		// Fallback se não conseguir parsear
		return enrollURL + ":8081"
	}
	return fmt.Sprintf("%s:8081", u.Hostname())
}

func loadConfig(path string) Config {
	var cfg Config
	if fileExists(path) {
		if _, err := toml.DecodeFile(path, &cfg); err != nil {
			log.Printf("⚠️ Erro ao ler config TOML: %v", err)
		}
	}
	return cfg
}

// --- LÓGICA DE CERTIFICADOS E MATRÍCULA ---

func ensureCACertificate(cfg Config) error {
	// Se arquivo já existe, ok
	if info, err := os.Stat(CaFile); err == nil && info.Size() > 0 {
		return nil
	}

	url := formatURL(cfg.Server.URL, "/ca.crt")
	log.Printf("[Boot] 📥 Baixando CA de %s...", url)

	// Cliente HTTP Inseguro (apenas para baixar o CA inicial)
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr, Timeout: 10 * time.Second}

	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("acesso negado ou erro (HTTP %d)", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	ensureCertDir()
	return os.WriteFile(CaFile, data, 0644)
}

func loadOrEnroll(cfg Config) (tls.Certificate, error) {
	// A) Tenta carregar do disco
	if fileExists(ClientCert) && fileExists(ClientKey) {
		cert, err := tls.LoadX509KeyPair(ClientCert, ClientKey)
		if err == nil {
			x509Cert, _ := x509.ParseCertificate(cert.Certificate[0])
			isExpired := time.Now().After(x509Cert.NotAfter)
			isNameMismatch := x509Cert.Subject.CommonName != cfg.Agent.ID

			if !isExpired && !isNameMismatch {
				log.Println("[Auth] ✅ Identidade válida carregada do disco.")
				return cert, nil
			}
			log.Println("[Auth] ⚠️  Certificado inválido, expirado ou nome mudou. Renovando...")
		}
	}

	// B) Matrícula (Enroll)
	log.Println("[Enroll] 📝 Solicitando novo certificado...")
	enrollURL := formatURL(cfg.Server.URL, "/enroll")

	caPool, err := loadLocalCA()
	if err != nil {
		return tls.Certificate{}, err
	}

	// Configura TLS com CA confiável (ou Insecure se configurado)
	tlsConfig := &tls.Config{RootCAs: caPool}
	if cfg.Server.Insecure {
		tlsConfig.InsecureSkipVerify = true
	}

	client := &http.Client{
		Transport: &http.Transport{TLSClientConfig: tlsConfig},
		Timeout:   10 * time.Second,
	}

	payload := map[string]string{
		"token": cfg.Agent.Token,
		"name":  cfg.Agent.ID,
		"group": cfg.Agent.Group,
	}
	jsonData, _ := json.Marshal(payload)

	// CORREÇÃO CRÍTICA: Criar Request manual para injetar o Header
	req, err := http.NewRequest("POST", enrollURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return tls.Certificate{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	// --- AQUI VAI O SEGREDO DA BLINDAGEM ---
	if cfg.Server.Secret != "" {
		req.Header.Set("X-App-Secret", cfg.Server.Secret)
	}

	resp, err := client.Do(req)
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

func connectTunnel(addr string, cert tls.Certificate, cfg Config) error {
	caPool, err := loadLocalCA()
	if err != nil {
		return err
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caPool,
	}
	// Se Insecure=true, ignora validação de hostname no túnel também
	if cfg.Server.Insecure {
		tlsConfig.InsecureSkipVerify = true
	}

	conn, err := tls.Dial("tcp", addr, tlsConfig)
	if err != nil {
		return err
	}

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

	stream.SetReadDeadline(time.Now().Add(10 * time.Second))
	br := bufio.NewReader(stream)
	req, err := http.ReadRequest(br)
	stream.SetReadDeadline(time.Time{})

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

	errChan := make(chan error, 2)
	go func() { _, err := io.Copy(dest, stream); errChan <- err }()
	go func() { _, err := io.Copy(stream, dest); errChan <- err }()
	<-errChan
}

func handleHTTP(stream net.Conn, req *http.Request) {
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse },
		Timeout:       30 * time.Second,
	}

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
