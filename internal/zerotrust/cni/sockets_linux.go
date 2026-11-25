package cni

import (
	"context"
	"errors"
	"net"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
)

func (a Attachment) ListenProxy(ctx context.Context, destination Destination) (net.Listener, error) {
	if !a.Live() {
		return nil, errors.New("session CNI attachment is unavailable")
	}
	config := net.ListenConfig{Control: a.bindSocket}
	return config.Listen(ctx, "tcp4", net.JoinHostPort(a.HostIP.String(), strconv.Itoa(int(destination.HostPort))))
}

func (a Attachment) DialControl(ctx context.Context) (net.Conn, error) {
	if !a.Live() {
		return nil, errors.New("session CNI attachment is unavailable")
	}
	dialer := net.Dialer{Control: a.bindSocket}
	return dialer.DialContext(ctx, "tcp4", net.JoinHostPort(a.PodIP.String(), strconv.Itoa(int(controlPort))))
}

func (a Attachment) bindSocket(_, _ string, raw syscall.RawConn) error {
	if !a.Live() {
		return errors.New("session interface changed before socket binding")
	}
	var socketErr error
	err := raw.Control(func(fd uintptr) {
		if socketErr = unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, a.Device); socketErr != nil {
			return
		}
		socketErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, SocketMark)
	})
	return errors.Join(err, socketErr)
}
