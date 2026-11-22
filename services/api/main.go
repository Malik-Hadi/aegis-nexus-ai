package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// loadEnv reads KEY=VALUE lines from .env (run the API from the project root).
func loadEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		if os.Getenv(k) == "" {
			os.Setenv(k, strings.TrimSpace(v))
		}
	}
}

func main() {
	loadEnv(".env")

	addr := os.Getenv("API_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	certFile, keyFile := os.Getenv("API_TLS_CERT"), os.Getenv("API_TLS_KEY")
	if (certFile == "") != (keyFile == "") {
		log.Fatal("set both API_TLS_CERT and API_TLS_KEY")
	}
	if certFile == "" {
		host, port, err := net.SplitHostPort(addr)
		if err == nil && host == "" {
			addr = net.JoinHostPort("127.0.0.1", port)
			host = "127.0.0.1"
		}
		ip := net.ParseIP(host)
		if err != nil || (host != "localhost" && !ip.IsLoopback()) {
			log.Fatal("plain HTTP is restricted to the loopback lab; set API_TLS_CERT and API_TLS_KEY")
		}
	}
	secret := os.Getenv("SESSION_SECRET")
	if len(secret) < 32 {
		log.Fatal("SESSION_SECRET must be at least 32 characters (see .env)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatalf("database config error: %v", err)
	}
	defer pool.Close()

	// Dummy hash so a login for an unknown user takes about as long as a real one.
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	dummy, _ := bcrypt.GenerateFromPassword([]byte(hex.EncodeToString(b)), bcrypt.DefaultCost)

	srv := &Server{
		pool:         pool,
		secret:       []byte(secret),
		secureCookie: certFile != "" || os.Getenv("COOKIE_SECURE") == "true",
		dummyHash:    dummy,
		hub:          newHub(),
	}

	iocPath := os.Getenv("IOC_FILE")
	if iocPath == "" {
		iocPath = "simulations/ioc_hashes.txt"
	}
	ioc, err := loadIOC(iocPath)
	if err != nil {
		log.Printf("warning: IOC list not loaded (%v); DET-004 is inactive", err)
	}
	srv.ioc = ioc
	go srv.monitorHeartbeats(context.Background())
	go srv.runAnalysisJobs(context.Background())

	log.Printf("AEGIS API listening on %s", addr)
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           limitRequests(srv.routes()),
		TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS12},
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 16,
	}
	if certFile != "" {
		log.Fatal(httpSrv.ListenAndServeTLS(certFile, keyFile))
	}
	log.Fatal(httpSrv.ListenAndServe())
}
