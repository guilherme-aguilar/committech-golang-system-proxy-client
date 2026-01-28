package main

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"crypto/x509"
	_ "embed"
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

//go:embed certs/ca.crt
var embeddedCA []byte

// Estrutura do arquivo TOML
type Config struct {
	ServerIP       string `toml:"server_ip"`
	Token          string `toml:"token"`
	Name           string `toml:"name"` // Identidade única
	ControlPort    string `toml:"control_port"`
	EnrollPort     string `toml:"enroll_port"`
	ReconnectDelay int    `toml:"reconnect_delay"`
}

func main() {
	// Flags para override (Útil para testes ou múltiplos containers)
	flagPath := flag.String("config", "client.toml", "Caminho do arquivo de configuração")
	flagIP := flag.String("server", "", "Sobrescreve o IP do servidor")
	flagToken := flag.String("token", "", "Sobrescreve o Token")
	flagName := flag.String("name", "", "Sobrescreve o Nome da Proxy")
	flag.Parse()

	// 1. Carregar Configuração
	cfg := loadConfig(*flagPath)

	// Aplicar Overrides
	if *flagIP != "" {
		cfg.ServerIP = *flagIP
	}
	if *flagToken != "" {
		cfg.Token = *flagToken
	}
	if *flagName != "" {
		cfg.Name = *flagName
	}

	// Validações
	if cfg.Token == "" {
		log.Fatal("Erro: Token é obrigatório")
	}
	if cfg.ServerIP == "" {
		log.Fatal("Erro: Server IP é obrigatório")
	}

	// Gera nome automático se estiver vazio
	if cfg.Name == "" {
		host, _ := os.Hostname()
		cfg.Name = "proxy-" + host
		log.Printf("[Config] Nome não definido, usando: %s", cfg.Name)
	}

	enrollURL := fmt.Sprintf("https://%s%s/enroll", cfg.ServerIP, cfg.EnrollPort)
	tunnelAddr := fmt.Sprintf("%s%s", cfg.ServerIP, cfg.ControlPort)

	log.Printf("[Init] Iniciando %s -> %s", cfg.Name, cfg.ServerIP)

	// 2. Matrícula (Enrollment)
	log.Println("[Enroll] Solicitando certificado...")
	clientCert, err := enroll(cfg.Token, cfg.Name, enrollURL)
	if err != nil {
		log.Fatalf("[Enroll] Falha fatal: %v", err)
	}
	log.Println("[Enroll] Identidade recebida com sucesso!")

	// 3. Loop de Conexão (Túnel)
	for {
		log.Printf("[Conn] Conectando ao túnel %s...", tunnelAddr)
		err := connectTunnel(tunnelAddr, clientCert)

		log.Printf("[Err] Desconectado: %v", err)
		log.Printf("[Wait] Reconectando em %ds...", cfg.ReconnectDelay)
		time.Sleep(time.Duration(cfg.ReconnectDelay) * time.Second)
	}
}

// Solicita o certificado ao servidor enviando Token e Nome
func enroll(token, name, url string) (tls.Certificate, error) {
	caPool := x509.NewCertPool()
	caPool.AppendCertsFromPEM(embeddedCA)

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: caPool},
		},
		Timeout: 10 * time.Second,
	}

	payload := map[string]string{
		"token": token,
		"name":  name,
	}
	jsonData, _ := json.Marshal(payload)

	resp, err := client.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return tls.Certificate{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return tls.Certificate{}, fmt.Errorf("Status %d: %s", resp.StatusCode, body)
	}

	var res struct {
		Cert string
		Key  string
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return tls.Certificate{}, err
	}

	return tls.X509KeyPair([]byte(res.Cert), []byte(res.Key))
}

// Conecta ao túnel mTLS
func connectTunnel(addr string, cert tls.Certificate) error {
	caPool := x509.NewCertPool()
	caPool.AppendCertsFromPEM(embeddedCA)

	config := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caPool,
	}

	conn, err := tls.Dial("tcp", addr, config)
	if err != nil {
		return err
	}
	defer conn.Close()

	session, err := yamux.Server(conn, nil)
	if err != nil {
		return err
	}

	log.Println("[Tunnel] 🔒 Conectado e aguardando requisições.")

	for {
		stream, err := session.Accept()
		if err != nil {
			return err
		}
		go handleStream(stream)
	}
}

// Processa a requisição que veio do servidor
func handleStream(stream net.Conn) {
	defer stream.Close()

	stream.SetReadDeadline(time.Now().Add(30 * time.Second))
	br := bufio.NewReader(stream)
	req, err := http.ReadRequest(br)
	stream.SetReadDeadline(time.Time{}) // Remove timeout

	if err != nil {
		return
	}

	log.Printf("[Request] %s %s", req.Method, req.Host)

	if req.Method == http.MethodConnect {
		// HTTPS / TCP Forwarding
		dest, err := net.DialTimeout("tcp", req.Host, 10*time.Second)
		if err != nil {
			return
		}
		defer dest.Close()
		go io.Copy(dest, stream)
		io.Copy(stream, dest)
	} else {
		// HTTP Forwarding
		client := &http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
			Timeout: 30 * time.Second,
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
}

func loadConfig(path string) Config {
	cfg := Config{
		ControlPort:    ":8081",
		EnrollPort:     ":8082",
		ReconnectDelay: 5,
	}
	if _, err := os.Stat(path); err == nil {
		toml.DecodeFile(path, &cfg)
	}
	return cfg
}
