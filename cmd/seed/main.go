package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

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
		if k, v, ok := strings.Cut(line, "="); ok && os.Getenv(strings.TrimSpace(k)) == "" {
			os.Setenv(strings.TrimSpace(k), strings.TrimSpace(v))
		}
	}
}

func main() {
	loadEnv(".env")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	const tenantA = "11111111-1111-1111-1111-111111111111"
	const tenantB = "22222222-2222-2222-2222-222222222222"
	users := []struct{ tenant, subject, role string }{
		{tenantA, "admin-a", "admin"},
		{tenantA, "analyst-a", "analyst"},
		{tenantB, "admin-b", "admin"},
		{tenantB, "analyst-b", "analyst"},
	}

	fmt.Println("Lab users (new random passwords). Save them in a private note, NOT in Git:")
	for _, u := range users {
		b := make([]byte, 8)
		if _, err := rand.Read(b); err != nil {
			log.Fatal(err)
		}
		pw := hex.EncodeToString(b)
		hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
		if err != nil {
			log.Fatal(err)
		}
		_, err = pool.Exec(ctx,
			`INSERT INTO users (tenant_id, subject, password_hash, role)
			 VALUES ($1::text::uuid, $2, $3, $4)
			 ON CONFLICT (tenant_id, subject)
			 DO UPDATE SET password_hash = EXCLUDED.password_hash, role = EXCLUDED.role, status = 'active'`,
			u.tenant, u.subject, string(hash), u.role)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("tenant=%s  user=%-10s role=%-8s password=%s\n", u.tenant, u.subject, u.role, pw)
	}
}
