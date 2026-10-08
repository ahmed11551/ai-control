package server

import (
	"crypto/tls"
	"crypto/x509"
	"embed"
	"log"
	"net/http"
	"sync"
	"time"
)

//go:embed maxcerts/*.crt
var maxCACerts embed.FS

var (
	maxHTTPOnce   sync.Once
	maxHTTPBase   *http.Transport
	maxHTTPInitEr error
)

func maxBotTransport() *http.Transport {
	maxHTTPOnce.Do(func() {
		roots, err := x509.SystemCertPool()
		if err != nil || roots == nil {
			roots = x509.NewCertPool()
		}
		entries, err := maxCACerts.ReadDir("maxcerts")
		if err != nil {
			maxHTTPInitEr = err
			return
		}
		n := 0
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			raw, err := maxCACerts.ReadFile("maxcerts/" + e.Name())
			if err != nil {
				continue
			}
			if roots.AppendCertsFromPEM(raw) {
				n++
			}
		}
		if n == 0 {
			log.Println("MAX Bot: CA Минцифры не загружены")
		}
		base, _ := http.DefaultTransport.(*http.Transport)
		tr := base.Clone()
		tr.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
		// Long poll: avoid sticky broken keep-alive sockets.
		tr.DisableKeepAlives = true
		maxHTTPBase = tr
	})
	return maxHTTPBase
}

func maxBotHTTPClient(timeout time.Duration) *http.Client {
	tr := maxBotTransport()
	if tr == nil {
		return &http.Client{Timeout: timeout}
	}
	return &http.Client{Timeout: timeout, Transport: tr}
}
