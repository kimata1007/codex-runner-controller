package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/kimata1007/codex-runner-controller/internal/zerotrust"
	"k8s.io/apimachinery/pkg/types"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("a fixture mode is required")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer cancel()
	switch args[0] {
	case "runner":
		<-ctx.Done()
		return nil
	case "session":
		server := &http.Server{Addr: ":8081", ReadHeaderTimeout: 3 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, "session control reached")
		})}
		return server.ListenAndServe()
	case "model":
		path := zerotrust.ServiceSocket(zerotrust.NodeRuntimeDirectory, "model", types.UID(os.Getenv("POD_UID")))
		listener, err := zerotrust.ListenPrivate(path)
		if err != nil {
			return err
		}
		defer listener.Close()
		server := &http.Server{ReadHeaderTimeout: 3 * time.Second, ConnContext: zerotrust.ContextWithLocalIdentity,
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				identity, ok := zerotrust.LocalIdentity(r.Context())
				if !ok {
					http.Error(w, "missing local identity", http.StatusForbidden)
					return
				}
				fmt.Fprint(w, identity)
			})}
		return server.Serve(zerotrust.AuthenticatedListener(listener, "codex.local"))
	case "control", "fetch", "connect", "deny":
		if len(args) != 2 {
			return errors.New("a fixture target is required")
		}
		ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
		defer cancel()
		if args[0] == "connect" || args[0] == "deny" {
			conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", args[1])
			if err == nil {
				conn.Close()
			}
			if args[0] == "deny" {
				if err == nil {
					return errors.New("unexpected direct connection")
				}
				return nil
			}
			return err
		}
		transport := &http.Transport{}
		url := args[1]
		if args[0] == "control" {
			url = "http://session/control"
			transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
				return zerotrust.DialSessionControl(ctx, filepath.Join(zerotrust.NodeRuntimeDirectory, "runner-api.sock"), types.UID(args[1]))
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		response, err := (&http.Client{Transport: transport}).Do(req)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("unexpected fixture response %d", response.StatusCode)
		}
		_, err = io.Copy(os.Stdout, io.LimitReader(response.Body, 4096))
		return err
	default:
		return errors.New("unknown fixture mode")
	}
}
