package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/kimata1007/codex-runner-controller/internal/zerotrust"
)

func main() {
	mode := flag.String("mode", "", "inbound or outbound")
	listen := flag.String("listen", "", "TCP address for inbound or private Unix socket for outbound")
	upstream := flag.String("upstream", "", "trusted upstream address")
	domain := flag.String("trust-domain", "codex.local", "identity trust domain")
	source := flag.String("source", "", "trusted route source identity")
	destination := flag.String("destination", "", "trusted route destination identity")
	port := flag.Uint("port", 0, "authorized destination service port")
	cert := flag.String("cert", "", "workload certificate PEM")
	key := flag.String("key", "", "workload private key PEM")
	ca := flag.String("ca", "", "trusted CA bundle PEM")
	policy := flag.String("policy", "", "controller-owned policy snapshot")
	flag.Parse()

	if (*mode != "inbound" && *mode != "outbound") || *listen == "" || *upstream == "" || *port == 0 || *port > 65535 || *policy == "" {
		log.Fatal("invalid route configuration")
	}
	src, err := zerotrust.ParseIdentity(*source, *domain)
	if err != nil {
		log.Fatal(err)
	}
	dst, err := zerotrust.ParseIdentity(*destination, *domain)
	if err != nil {
		log.Fatal(err)
	}
	route := zerotrust.Route{Source: src, Destination: dst, Port: uint16(*port), Address: *upstream}
	relay := &zerotrust.Relay{Domain: *domain, Policies: zerotrust.FilePolicy{Path: *policy, Domain: *domain}}
	inbound := *mode == "inbound"
	localID, expected := src, dst
	if inbound {
		localID, expected = dst, src
	}
	loadTLS := func() (*tls.Config, error) {
		pair, err := tls.LoadX509KeyPair(*cert, *key)
		if err != nil {
			return nil, err
		}
		leaf, err := x509.ParseCertificate(pair.Certificate[0])
		if err != nil {
			return nil, err
		}
		if len(leaf.URIs) != 1 || leaf.URIs[0].String() != string(localID) || time.Now().Before(leaf.NotBefore) || !time.Now().Before(leaf.NotAfter) {
			return nil, errors.New("invalid local workload certificate")
		}
		pem, err := os.ReadFile(*ca)
		if err != nil {
			return nil, err
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(pem) {
			return nil, errors.New("invalid CA bundle")
		}
		return zerotrust.TLSConfig(pair, roots, *domain, expected, inbound)
	}
	if _, err := loadTLS(); err != nil {
		log.Fatal(err)
	}

	var listener net.Listener
	if inbound {
		listener, err = net.Listen("tcp", *listen)
	} else {
		// Only the trusted interceptor may access this socket. Never mount
		// this directory into a session pod. No automatic unlink of old paths.
		dir, statErr := os.Stat(filepath.Dir(*listen))
		if statErr != nil || !dir.IsDir() || dir.Mode().Perm() != 0700 {
			log.Fatal("outbound socket requires a private 0700 directory")
		}
		listener, err = net.Listen("unix", *listen)
		if err == nil {
			err = os.Chmod(*listen, 0600)
		}
	}
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	slots := make(chan struct{}, 128)
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Fatal(err)
		}
		select {
		case slots <- struct{}{}:
		default:
			_ = conn.Close()
			continue
		}
		go func() {
			defer func() { <-slots }()
			defer conn.Close()
			cfg, err := loadTLS()
			if err == nil {
				if inbound {
					err = relay.Inbound(ctx, tls.Server(conn, cfg), route)
				} else {
					err = relay.Outbound(ctx, conn, route, cfg)
				}
			}
			if err != nil {
				log.Printf("route closed: source=%s destination=%s port=%d", src, dst, route.Port)
			}
		}()
	}
}
