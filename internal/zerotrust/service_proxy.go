package zerotrust

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	v1alpha1 "github.com/kimata1007/codex-runner-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func InClusterProxyClient() (client.Client, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	config.Timeout = 3 * time.Second
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{corev1.AddToScheme, v1alpha1.AddToScheme} {
		if err := add(scheme); err != nil {
			return nil, err
		}
	}
	return client.New(config, client.Options{Scheme: scheme})
}

func ServicePath(role, domain string, podUID types.UID) (string, error) {
	if !component.MatchString(string(podUID)) {
		return "", errors.New("service proxy needs a Kubernetes Pod UID")
	}
	if _, err := ParseIdentity("spiffe://"+domain+"/proxy/"+role, domain); err != nil {
		return "", err
	}
	return ServiceSocket(NodeRuntimeDirectory, role, podUID), nil
}

func CheckServiceSocket(path string) error {
	conn, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		return err
	}
	return conn.Close()
}

func ServeProxy(ctx context.Context, socket, domain string, handler http.Handler) error {
	listener, err := ListenPrivate(socket)
	if err != nil {
		return err
	}
	defer listener.Close()
	server := &http.Server{Handler: handler, ConnContext: ContextWithLocalIdentity,
		ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 8 * time.Second, WriteTimeout: 13 * time.Minute,
		IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	done := make(chan error, 1)
	go func() { done <- server.Serve(AuthenticatedListener(listener, domain)) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
		}
		<-done
		return nil
	}
}

func MountedCredential(path string) func() (string, error) {
	return func() (string, error) {
		value, err := os.ReadFile(path)
		key := strings.TrimSpace(string(value))
		if err != nil || len(value) > 4096 || key == "" || strings.ContainsAny(key, " \t\r\n") {
			return "", errors.New("service proxy credential unavailable")
		}
		return key, nil
	}
}
