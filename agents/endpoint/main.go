package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"runtime"
	"strings"
	"time"
)

var badHostChars = regexp.MustCompile(`[^A-Za-z0-9._-]`)

func sanitizeHostname(h string) string {
	h = badHostChars.ReplaceAllString(h, "-")
	if len(h) > 100 {
		h = h[:100]
	}
	if h == "" {
		h = "lab-agent"
	}
	return h
}

func checkAPI(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("invalid -api %q (use http://host:port or https://host)", raw)
	}
	host := u.Hostname()
	if u.Scheme == "http" && host != "localhost" && host != "127.0.0.1" && host != "host.docker.internal" {
		log.Printf("WARNING: plain HTTP to %s. Use HTTPS outside the lab.", host)
	}
	return strings.TrimRight(raw, "/"), nil
}

func usage() {
	fmt.Println(`AEGIS lab agent
Usage:
  agent enrol -token <one-time-token> [-api http://localhost:8080] [-hostname NAME] [-state agent-state.json] [-force]
  agent run   [-interval 30s] [-replay fixture.json] [-once] [-state agent-state.json] [-queue agent-queue.jsonl]
Lab use only: send synthetic events from machines you own or are authorised to test.`)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "enrol":
		os.Exit(cmdEnrol(os.Args[2:]))
	case "run":
		os.Exit(cmdRun(os.Args[2:]))
	default:
		usage()
		os.Exit(2)
	}
}

func cmdEnrol(args []string) int {
	fs := flag.NewFlagSet("enrol", flag.ContinueOnError)
	api := fs.String("api", "http://localhost:8080", "API base URL")
	token := fs.String("token", "", "one-time enrolment token")
	host := fs.String("hostname", "", "agent hostname (default: this machine)")
	statePath := fs.String("state", "agent-state.json", "where to save the credential")
	force := fs.Bool("force", false, "overwrite an existing state file")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *token == "" {
		fmt.Fprintln(os.Stderr, "missing -token")
		return 2
	}
	base, err := checkAPI(*api)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if _, err := os.Stat(*statePath); err == nil && !*force {
		fmt.Fprintf(os.Stderr, "%s already exists. Use -force to enrol again.\n", *statePath)
		return 2
	}
	name := *host
	if name == "" {
		name, _ = os.Hostname()
	}
	name = sanitizeHostname(name)
	osName := runtime.GOOS
	if osName != "windows" && osName != "linux" {
		fmt.Fprintf(os.Stderr, "unsupported OS %q (lab agent supports windows and linux)\n", osName)
		return 2
	}

	c := newClient(base, "")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	status, body, err := c.postN(ctx, "/v1/agents/enrol", map[string]string{"token": *token, "hostname": name, "os": osName}, c.maxAttempts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "enrol failed: %v\n", err)
		return 1
	}
	if status == 401 {
		fmt.Fprintln(os.Stderr, "enrol refused: the token is invalid, already used, or expired")
		return 1
	}
	if status != 201 {
		fmt.Fprintf(os.Stderr, "enrol failed (%d): %s\n", status, truncate(string(body)))
		return 1
	}
	var resp struct {
		AgentID    string `json:"agent_id"`
		Credential string `json:"credential"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || resp.Credential == "" {
		fmt.Fprintln(os.Stderr, "unexpected response from server")
		return 1
	}
	if err := saveState(*statePath, state{AgentID: resp.AgentID, Credential: resp.Credential, API: base}); err != nil {
		fmt.Fprintf(os.Stderr, "could not save state: %v\n", err)
		return 1
	}
	fmt.Printf("Enrolled as agent %s (%s, %s).\nCredential saved to %s. Keep that file private.\n", resp.AgentID, name, osName, *statePath)
	return 0
}

func cmdRun(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	statePath := fs.String("state", "agent-state.json", "credential file created by 'enrol'")
	queuePath := fs.String("queue", "agent-queue.jsonl", "offline queue file")
	interval := fs.Duration("interval", 30*time.Second, "heartbeat interval")
	replay := fs.String("replay", "", "fixture file to send as synthetic events")
	rebase := fs.Bool("rebase", true, "shift fixture timestamps so the last event is 'now'")
	unique := fs.Bool("unique", true, "make idempotency keys unique for this run")
	once := fs.Bool("once", false, "send the fixture, flush the queue, then exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *interval < time.Second {
		fmt.Fprintln(os.Stderr, "-interval must be at least 1s")
		return 2
	}
	st, err := loadState(*statePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot read %s (run 'enrol' first): %v\n", *statePath, err)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	a := &agent{c: newClient(st.API, st.Credential), queuePath: *queuePath}

	if *replay != "" {
		evs, err := loadFixture(*replay)
		if err != nil {
			fmt.Fprintf(os.Stderr, "fixture error: %v\n", err)
			return 2
		}
		rb := make([]byte, 3)
		_, _ = rand.Read(rb)
		evs, err = prepare(evs, time.Now().UTC(), *rebase, *unique, hex.EncodeToString(rb))
		if err != nil {
			fmt.Fprintf(os.Stderr, "fixture error: %v\n", err)
			return 2
		}
		counts := map[string]int{}
		for _, ev := range evs {
			res, err := a.submit(ctx, ev)
			if errors.Is(err, errUnauthorized) {
				fmt.Fprintln(os.Stderr, err)
				return 3
			}
			counts[res]++
		}
		fmt.Printf("Replay finished: %d sent, %d queued, %d dropped.\n", counts["sent"], counts["queued"], counts["dropped"])
	}

	beat := func() int {
		status, _, err := a.c.postN(ctx, "/v1/agents/heartbeat", nil, 1)
		switch {
		case err != nil:
			log.Printf("heartbeat failed: %v", err)
		case status == 401:
			fmt.Fprintln(os.Stderr, errUnauthorized)
			return 3
		case status == 200:
			log.Printf("heartbeat ok")
		default:
			log.Printf("heartbeat returned %d", status)
		}
		n, err := a.flush(ctx)
		if errors.Is(err, errUnauthorized) {
			fmt.Fprintln(os.Stderr, err)
			return 3
		}
		if err != nil {
			log.Printf("queue error: %v", err)
		}
		if n > 0 {
			log.Printf("sent %d queued events", n)
		}
		return 0
	}

	if *once {
		return beat()
	}
	if code := beat(); code != 0 {
		return code
	}
	ticker := time.NewTicker(*interval)
	defer ticker.Stop()
	fmt.Printf("Agent running (heartbeat every %s). Press Ctrl+C to stop.\n", *interval)
	for {
		select {
		case <-ctx.Done():
			fmt.Println("Agent stopping.")
			return 0
		case <-ticker.C:
			if code := beat(); code != 0 {
				return code
			}
		}
	}
}
