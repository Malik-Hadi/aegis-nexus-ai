package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
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
		addr = ":8080"
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
		secureCookie: os.Getenv("COOKIE_SECURE") == "true",
		dummyHash:    dummy,
	}

	log.Printf("AEGIS API listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, srv.routes()))
}
