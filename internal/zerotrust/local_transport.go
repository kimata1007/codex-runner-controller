package zerotrust

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"k8s.io/apimachinery/pkg/types"
)

const NodeRuntimeDirectory = "/run/codex-node"

type localConnection struct {
	net.Conn
	reader   *bufio.Reader
	identity Identity
}

func (c *localConnection) Read(p []byte) (int, error) { return c.reader.Read(p) }

func (c *localConnection) CloseWrite() error {
	if conn, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return conn.CloseWrite()
	}
	return c.Close()
}

func routePreface(conn net.Conn) (*bufio.Reader, string, error) {
	reader := bufio.NewReaderSize(conn, 640)
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		return nil, "", err
	}
	defer conn.SetReadDeadline(time.Time{})
	preface, err := reader.ReadSlice('\n')
	if err != nil || len(preface) < 2 {
		return nil, "", errors.New("invalid local route preface")
	}
	return reader, string(preface[:len(preface)-1]), nil
}

func writePreface(conn net.Conn, value string) error {
	if err := conn.SetWriteDeadline(time.Now().Add(3 * time.Second)); err != nil {
		return err
	}
	defer conn.SetWriteDeadline(time.Time{})
	_, err := io.WriteString(conn, value+"\n")
	return err
}

func DialSessionControl(ctx context.Context, socket string, uid types.UID) (net.Conn, error) {
	if !component.MatchString(string(uid)) {
		return nil, ErrPolicyDenied
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", socket)
	if err != nil {
		return nil, err
	}
	if err := writePreface(conn, string(uid)); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func ServiceSocket(directory, role string, uid types.UID) string {
	return filepath.Join(directory, role, string(uid)+".sock")
}

type serviceListener struct {
	net.Listener
	domain string
}

// AuthenticatedListener accepts local sockets exclusively from the Node Proxy.
// Its directory must be shared with the trusted proxy and excluded from all
// untrusted workloads.
func AuthenticatedListener(listener net.Listener, domain string) net.Listener {
	return serviceListener{Listener: listener, domain: domain}
}

func (l serviceListener) Accept() (net.Conn, error) {
	if l.Listener.Addr().Network() != "unix" {
		return nil, errors.New("authenticated local listener requires a Unix socket")
	}
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		reader, preface, err := routePreface(conn)
		if err == nil {
			id, parseErr := ParseIdentity(preface, l.domain)
			if parseErr == nil {
				return &localConnection{Conn: conn, reader: reader, identity: id}, nil
			}
		}
		conn.Close()
	}
}

type localIdentityKey struct{}

func ContextWithLocalIdentity(ctx context.Context, conn net.Conn) context.Context {
	if c, ok := conn.(*localConnection); ok && c.identity != "" {
		return context.WithValue(ctx, localIdentityKey{}, c.identity)
	}
	return ctx
}

func LocalIdentity(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(localIdentityKey{}).(Identity)
	return id, ok
}

func ListenPrivate(path string) (net.Listener, error) {
	dir, err := os.Lstat(filepath.Dir(path))
	if err != nil || !dir.IsDir() || dir.Mode().Perm()&0007 != 0 {
		return nil, errors.New("local socket requires a private directory")
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0660 {
			return nil, errors.New("unsafe local socket path")
		}
		conn, dialErr := net.DialTimeout("unix", path, 100*time.Millisecond)
		if dialErr == nil {
			conn.Close()
			return nil, errors.New("local listener is already running")
		}
		if !errors.Is(dialErr, syscall.ECONNREFUSED) {
			return nil, dialErr
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0660); err != nil {
		listener.Close()
		return nil, err
	}
	return listener, nil
}
