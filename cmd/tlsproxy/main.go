// Lab TLS terminator: HTTPS (self-signed) -> local API over loopback. Lab use only.
package main

import (
    "crypto/ecdsa"
    "crypto/elliptic"
    "crypto/rand"
    "crypto/tls"
    "crypto/x509"
    "crypto/x509/pkix"
    "log"
    "math/big"
    "net"
    "net/http"
    "net/http/httputil"
    "net/url"
    "os"
    "time"
)

func env(k, d string) string {
    if v := os.Getenv(k); v != "" {
        return v
    }
    return d
}

func main() {
    target, _ := url.Parse(env("UPSTREAM", "http://127.0.0.1:8080"))
    key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
    tpl := &x509.Certificate{
        SerialNumber: big.NewInt(time.Now().UnixNano()),
        Subject:      pkix.Name{CommonName: "aegis-lab-localhost"},
        NotBefore:    time.Now().Add(-time.Minute),
        NotAfter:     time.Now().Add(72 * time.Hour),
        KeyUsage:     x509.KeyUsageDigitalSignature,
        ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
        DNSNames:     []string{"localhost"},
        IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
    }
    der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
    if err != nil {
        log.Fatal(err)
    }
    proxy := httputil.NewSingleHostReverseProxy(target)
    proxy.FlushInterval = -1 // keep SSE streaming live
    orig := proxy.Director
    proxy.Director = func(r *http.Request) { orig(r); r.Header.Set("X-Forwarded-Proto", "https") }
    h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Strict-Transport-Security", "max-age=31536000")
        proxy.ServeHTTP(w, r)
    })
    srv := &http.Server{
        Addr:              env("LISTEN", "127.0.0.1:8443"),
        Handler:           h,
        ReadHeaderTimeout: 10 * time.Second,
        TLSConfig: &tls.Config{
            MinVersion:   tls.VersionTLS12,
            Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
        },
    }
    log.Printf("TLS proxy on https://%s -> %s", srv.Addr, target)
    log.Fatal(srv.ListenAndServeTLS("", ""))
}
