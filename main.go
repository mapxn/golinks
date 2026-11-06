// Command golinks is a single-machine "go links" service: type go/<key> in
// the browser address bar and land on the mapped URL. See README.md.
package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"time"
)

// serveOnLoopback starts one HTTP server per loopback address (a separate
// http.Server per listener — a single Server must not Serve twice). The
// IPv4 listener is mandatory; an IPv6 failure is logged but not fatal.
func serveOnLoopback(name string, port int, makeServer func() *http.Server) <-chan error {
	errCh := make(chan error, 4)
	addrs := []string{
		fmt.Sprintf("127.0.0.1:%d", port),
		fmt.Sprintf("[::1]:%d", port),
	}
	for i, addr := range addrs {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			if i == 0 {
				errCh <- fmt.Errorf("%s listen %s: %w", name, addr, err)
			} else {
				log.Printf("%s: cannot listen on %s (%v); IPv4 loopback only", name, addr, err)
			}
			continue
		}
		srv := makeServer()
		go func(ln net.Listener, srv *http.Server) {
			var err error
			if srv.TLSConfig != nil {
				// Certificates come from TLSConfig; no files to load here.
				err = srv.ServeTLS(ln, "", "")
			} else {
				err = srv.Serve(ln)
			}
			if err != nil && err != http.ErrServerClosed {
				errCh <- fmt.Errorf("%s serve %s: %w", name, ln.Addr(), err)
			}
		}(ln, srv)
		log.Printf("%s listening on %s", name, addr)
	}
	return errCh
}

func newServer(handler http.Handler, tlsCfg *tls.Config) *http.Server {
	return &http.Server{
		Handler:           handler,
		TLSConfig:         tlsCfg,
		ReadHeaderTimeout: 10 * time.Second,
	}
}

func run(cfg Config) error {
	store, err := loadStore(cfg.DataFile)
	if err != nil {
		return err
	}
	app := &server{store: store, cfg: cfg}

	var errChs []<-chan error

	if cfg.HTTPSEnabled {
		tlsCfg, err := cfg.tlsConfig()
		if err != nil {
			return err
		}
		errChs = append(errChs, serveOnLoopback("https", cfg.HTTPSPort, func() *http.Server {
			return newServer(app, tlsCfg)
		}))
	}
	if cfg.HTTPEnabled {
		var handler http.Handler = app
		if cfg.HTTPSEnabled {
			// Both on: HTTP only upgrades to HTTPS (requirements v2).
			handler = httpsRedirectHandler(cfg.HTTPSPort)
		}
		errChs = append(errChs, serveOnLoopback("http", cfg.HTTPPort, func() *http.Server {
			return newServer(handler, nil)
		}))
	}

	// Block on the first fatal listener error.
	fatal := make(chan error, len(errChs))
	for _, ch := range errChs {
		go func(c <-chan error) {
			for err := range c {
				fatal <- err
			}
		}(ch)
	}
	return <-fatal
}

// version is stamped at build time via -ldflags "-X main.version=..."
// (see the Makefile's dist target). Source builds report "dev".
var version = "dev"

func main() {
	configPath := flag.String("config", "config.json", "path to config file")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("golinks " + version)
		return
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "golinks:", err)
		os.Exit(1)
	}
	log.Printf("golinks starting: http=%v (port %d) https=%v (port %d) data=%s",
		cfg.HTTPEnabled, cfg.HTTPPort, cfg.HTTPSEnabled, cfg.HTTPSPort, cfg.DataFile)
	if err := run(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "golinks:", err)
		os.Exit(1)
	}
}
