package tunnel

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

const (
	sshDialTimeout     = 60 * time.Second
	forwardDialTimeout = 10 * time.Second
	reconnectInterval  = 10 * time.Second
	keepaliveInterval  = 10 * time.Second
	keepaliveTimeout   = 10 * time.Second
)

type RemoteForward struct {
	RemoteAddr string
	LocalAddr  string
	Events     <-chan ForwardEvent
	events     chan<- ForwardEvent
	retry      chan struct{}
	done       <-chan struct{}
	close      func()
}

type LocalForwardSpec struct {
	LocalAddr  string
	RemoteAddr string
}

type ForwardStatus string

const (
	ForwardStatusConnected    ForwardStatus = "connected"
	ForwardStatusReconnecting ForwardStatus = "reconnecting"
	ForwardStatusClosed       ForwardStatus = "closed"
)

type ForwardEvent struct {
	Status            ForwardStatus
	Err               error
	RetryIn           time.Duration
	ManualRetryFailed bool
}

func (f *RemoteForward) Close() {
	if f != nil && f.close != nil {
		f.close()
	}
}

func (f *RemoteForward) Retry() {
	if f == nil || f.retry == nil {
		return
	}
	select {
	case f.retry <- struct{}{}:
	default:
	}
}

func (f *RemoteForward) Wait(ctx context.Context) error {
	if f == nil || f.done == nil {
		return nil
	}
	select {
	case <-f.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// StartAutoReconnectRemoteForward starts a remote forward and recreates it every
// 10 seconds if the SSH connection or remote listener is interrupted.
func StartAutoReconnectRemoteForward(ctx context.Context, user string, sshAddr string, keyPath string, remoteAddr string, localAddr string) (*RemoteForward, error) {
	ctx, cancel := context.WithCancel(ctx)
	events := make(chan ForwardEvent, 16)
	retry := make(chan struct{}, 1)
	closed := make(chan struct{})

	done := make(chan error, 1)
	client, listener, err := startRemoteForwardOnce(ctx, user, sshAddr, keyPath, remoteAddr, localAddr, done)
	if err != nil {
		cancel()
		return nil, err
	}

	go func() {
		currentClient := client
		currentListener := listener
		defer close(events)
		defer close(closed)

		for {
			select {
			case <-ctx.Done():
				emitForwardEvent(events, ForwardStatusClosed, nil)
				_ = currentListener.Close()
				_ = currentClient.Close()
				return
			case err := <-done:
				emitForwardEvent(events, ForwardStatusReconnecting, err)
				_ = currentListener.Close()
				_ = currentClient.Close()
			}

			for {
				manual, err := waitBeforeReconnect(ctx, retry, func(remaining time.Duration) {
					emitForwardRetryEvent(events, remaining)
				})
				if err != nil {
					return
				}

				nextDone := make(chan error, 1)
				nextClient, nextListener, err := startRemoteForwardOnce(ctx, user, sshAddr, keyPath, remoteAddr, localAddr, nextDone)
				if err != nil {
					if manual {
						emitManualRetryFailure(events, err)
					}
					continue
				}

				currentClient = nextClient
				currentListener = nextListener
				done = nextDone
				emitForwardEvent(events, ForwardStatusConnected, nil)
				break
			}
		}
	}()

	return &RemoteForward{
		RemoteAddr: remoteAddr,
		LocalAddr:  localAddr,
		Events:     events,
		events:     events,
		retry:      retry,
		done:       closed,
		close:      cancel,
	}, nil
}

// StartAutoReconnectLocalForwards starts local port forwards and an optional
// SOCKS5 dynamic forward, then recreates them every 10 seconds if SSH breaks.
func StartAutoReconnectLocalForwards(ctx context.Context, user string, sshAddr string, keyPath string, forwards []LocalForwardSpec, socksAddr string) (*RemoteForward, error) {
	ctx, cancel := context.WithCancel(ctx)
	events := make(chan ForwardEvent, 16)
	retry := make(chan struct{}, 1)
	closed := make(chan struct{})

	done := make(chan error, 1)
	client, listeners, err := startLocalForwardGroupOnce(ctx, user, sshAddr, keyPath, forwards, socksAddr, done)
	if err != nil {
		cancel()
		return nil, err
	}

	go func() {
		currentClient := client
		currentListeners := listeners
		defer close(events)
		defer close(closed)

		for {
			select {
			case <-ctx.Done():
				emitForwardEvent(events, ForwardStatusClosed, nil)
				closeListeners(currentListeners)
				_ = currentClient.Close()
				return
			case err := <-done:
				emitForwardEvent(events, ForwardStatusReconnecting, err)
				closeListeners(currentListeners)
				_ = currentClient.Close()
			}

			for {
				manual, err := waitBeforeReconnect(ctx, retry, func(remaining time.Duration) {
					emitForwardRetryEvent(events, remaining)
				})
				if err != nil {
					return
				}

				nextDone := make(chan error, 1)
				nextClient, nextListeners, err := startLocalForwardGroupOnce(ctx, user, sshAddr, keyPath, forwards, socksAddr, nextDone)
				if err != nil {
					if manual {
						emitManualRetryFailure(events, err)
					}
					continue
				}

				currentClient = nextClient
				currentListeners = nextListeners
				done = nextDone
				emitForwardEvent(events, ForwardStatusConnected, nil)
				break
			}
		}
	}()

	return &RemoteForward{
		RemoteAddr: sshAddr,
		LocalAddr:  localForwardSummary(forwards, socksAddr),
		Events:     events,
		events:     events,
		retry:      retry,
		done:       closed,
		close:      cancel,
	}, nil
}

// StartAutoReconnectPasswordDynamicForward starts a SOCKS5 proxy through an
// SSH server authenticated with a password and reconnects if SSH is interrupted.
func StartAutoReconnectPasswordDynamicForward(ctx context.Context, user string, sshAddr string, password string, socksAddr string) (*RemoteForward, error) {
	ctx, cancel := context.WithCancel(ctx)
	events := make(chan ForwardEvent, 16)
	retry := make(chan struct{}, 1)
	closed := make(chan struct{})

	done := make(chan error, 1)
	client, listeners, err := startPasswordDynamicForwardOnce(ctx, user, sshAddr, password, socksAddr, done)
	if err != nil {
		cancel()
		return nil, err
	}

	go func() {
		currentClient := client
		currentListeners := listeners
		defer close(events)
		defer close(closed)

		for {
			select {
			case <-ctx.Done():
				emitForwardEvent(events, ForwardStatusClosed, nil)
				closeListeners(currentListeners)
				_ = currentClient.Close()
				return
			case err := <-done:
				emitForwardEvent(events, ForwardStatusReconnecting, err)
				closeListeners(currentListeners)
				_ = currentClient.Close()
			}

			for {
				manual, err := waitBeforeReconnect(ctx, retry, func(remaining time.Duration) {
					emitForwardRetryEvent(events, remaining)
				})
				if err != nil {
					return
				}

				nextDone := make(chan error, 1)
				nextClient, nextListeners, err := startPasswordDynamicForwardOnce(ctx, user, sshAddr, password, socksAddr, nextDone)
				if err != nil {
					if manual {
						emitManualRetryFailure(events, err)
					}
					continue
				}

				currentClient = nextClient
				currentListeners = nextListeners
				done = nextDone
				emitForwardEvent(events, ForwardStatusConnected, nil)
				break
			}
		}
	}()

	return &RemoteForward{
		RemoteAddr: sshAddr,
		LocalAddr:  "SOCKS5 " + socksAddr,
		Events:     events,
		events:     events,
		retry:      retry,
		done:       closed,
		close:      cancel,
	}, nil
}

// StartLocalForward forwards localAddr to remoteAddr through the SSH client.
func StartLocalForward(ctx context.Context, client *ssh.Client, localAddr string, remoteAddr string) error {
	if client == nil {
		return errors.New("ssh client is nil")
	}

	listener, err := net.Listen("tcp", localAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", localAddr, err)
	}
	defer listener.Close()

	var wg sync.WaitGroup
	defer wg.Wait()

	errCh := make(chan error, 1)

	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	for {
		localConn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}

			select {
			case errCh <- fmt.Errorf("accept local connection: %w", err):
			default:
			}
			return <-errCh
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			forwardConnection(client, localConn, remoteAddr)
		}()
	}
}

func StartRemoteForward(ctx context.Context, user string, sshAddr string, keyPath string, remoteAddr string, localAddr string) (*RemoteForward, error) {
	ctx, cancel := context.WithCancel(ctx)
	closed := make(chan struct{})

	done := make(chan error, 1)
	client, listener, err := startRemoteForwardOnce(ctx, user, sshAddr, keyPath, remoteAddr, localAddr, done)
	if err != nil {
		cancel()
		return nil, err
	}

	go func() {
		defer close(closed)
		<-ctx.Done()
		_ = listener.Close()
		_ = client.Close()
	}()

	return &RemoteForward{
		RemoteAddr: remoteAddr,
		LocalAddr:  localAddr,
		done:       closed,
		close:      cancel,
	}, nil
}

func startRemoteForwardOnce(ctx context.Context, user string, sshAddr string, keyPath string, remoteAddr string, localAddr string, done chan<- error) (*ssh.Client, net.Listener, error) {
	client, err := connectWithPrivateKey(ctx, user, sshAddr, keyPath)
	if err != nil {
		return nil, nil, err
	}

	monitorSSHConnection(ctx, client, done)

	go func() {
		<-ctx.Done()
		_ = client.Close()
	}()

	listener, err := client.Listen("tcp", remoteAddr)
	if err != nil {
		_ = client.Close()
		return nil, nil, fmt.Errorf("listen remote %s: %w", remoteAddr, err)
	}

	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	go acceptRemoteForward(ctx, listener, localAddr, done)

	return client, listener, nil
}

func startLocalForwardGroupOnce(ctx context.Context, user string, sshAddr string, keyPath string, forwards []LocalForwardSpec, socksAddr string, done chan<- error) (*ssh.Client, []net.Listener, error) {
	client, err := connectWithPrivateKey(ctx, user, sshAddr, keyPath)
	if err != nil {
		return nil, nil, err
	}

	monitorSSHConnection(ctx, client, done)

	listeners := make([]net.Listener, 0, len(forwards)+1)
	for _, forward := range forwards {
		listener, err := net.Listen("tcp", forward.LocalAddr)
		if err != nil {
			closeListeners(listeners)
			_ = client.Close()
			return nil, nil, fmt.Errorf("listen local %s: %w", forward.LocalAddr, err)
		}

		listeners = append(listeners, listener)
		go acceptLocalForward(ctx, client, listener, forward.RemoteAddr, done)
	}

	if socksAddr != "" {
		listener, err := net.Listen("tcp", socksAddr)
		if err != nil {
			closeListeners(listeners)
			_ = client.Close()
			return nil, nil, fmt.Errorf("listen socks %s: %w", socksAddr, err)
		}

		listeners = append(listeners, listener)
		go acceptSOCKS5(ctx, client, listener, done)
	}

	go func() {
		<-ctx.Done()
		closeListeners(listeners)
		_ = client.Close()
	}()

	return client, listeners, nil
}

func startPasswordDynamicForwardOnce(ctx context.Context, user string, sshAddr string, password string, socksAddr string, done chan<- error) (*ssh.Client, []net.Listener, error) {
	client, err := connectWithPassword(ctx, user, sshAddr, password)
	if err != nil {
		return nil, nil, err
	}

	monitorSSHConnection(ctx, client, done)

	listener, err := net.Listen("tcp", socksAddr)
	if err != nil {
		_ = client.Close()
		return nil, nil, fmt.Errorf("listen socks %s: %w", socksAddr, err)
	}
	listeners := []net.Listener{listener}
	go acceptSOCKS5(ctx, client, listener, done)

	go func() {
		<-ctx.Done()
		closeListeners(listeners)
		_ = client.Close()
	}()

	return client, listeners, nil
}

func connectWithPrivateKey(ctx context.Context, user string, sshAddr string, keyPath string) (*ssh.Client, error) {
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("read private key %s: %w", keyPath, err)
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("parse private key %s: %w", keyPath, err)
	}

	hostKeyCallback, err := knownHostsCallback()
	if err != nil {
		return nil, err
	}

	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: hostKeyCallback,
		Timeout:         sshDialTimeout,
	}

	dialer := net.Dialer{Timeout: sshDialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", sshAddr)
	if err != nil {
		return nil, fmt.Errorf("dial ssh %s as %s: %w", sshAddr, user, err)
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, sshAddr, config)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("establish ssh connection %s as %s using %s: %w", sshAddr, user, keyPath, err)
	}

	return ssh.NewClient(sshConn, chans, reqs), nil
}

func connectWithPassword(ctx context.Context, user string, sshAddr string, password string) (*ssh.Client, error) {
	host, _, err := net.SplitHostPort(sshAddr)
	if err != nil {
		return nil, fmt.Errorf("parse password SSH address %s: %w", sshAddr, err)
	}
	ip := net.ParseIP(host)
	if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
		return nil, fmt.Errorf("password SSH connection is restricted to localhost, got %s", host)
	}

	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
		},
		// The password proxy only connects through the local loopback tunnel.
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         sshDialTimeout,
	}

	dialer := net.Dialer{Timeout: sshDialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", sshAddr)
	if err != nil {
		return nil, fmt.Errorf("dial ssh %s as %s: %w", sshAddr, user, err)
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, sshAddr, config)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("establish ssh connection %s as %s using password: %w", sshAddr, user, err)
	}

	return ssh.NewClient(sshConn, chans, reqs), nil
}

func knownHostsCallback() (ssh.HostKeyCallback, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve user home dir: %w", err)
	}

	path := filepath.Join(homeDir, ".ssh", "known_hosts")
	callback, err := knownhosts.New(path)
	if err != nil {
		return nil, fmt.Errorf("load known_hosts %s: %w", path, err)
	}

	return callback, nil
}

func acceptRemoteForward(ctx context.Context, listener net.Listener, localAddr string, done chan<- error) {
	for {
		remoteConn, err := listener.Accept()
		if err != nil {
			notifyForwardDone(ctx, done, fmt.Errorf("accept remote forward connection: %w", err))
			return
		}

		go forwardRemoteConnection(ctx, remoteConn, localAddr)
	}
}

func acceptLocalForward(ctx context.Context, client *ssh.Client, listener net.Listener, remoteAddr string, done chan<- error) {
	for {
		localConn, err := listener.Accept()
		if err != nil {
			notifyForwardDone(ctx, done, fmt.Errorf("accept local forward connection: %w", err))
			return
		}

		go forwardLocalConnection(client, localConn, remoteAddr)
	}
}

func acceptSOCKS5(ctx context.Context, client *ssh.Client, listener net.Listener, done chan<- error) {
	for {
		localConn, err := listener.Accept()
		if err != nil {
			notifyForwardDone(ctx, done, fmt.Errorf("accept socks connection: %w", err))
			return
		}

		go handleSOCKS5(client, localConn)
	}
}

func notifyForwardDone(ctx context.Context, done chan<- error, err error) {
	if ctx.Err() != nil {
		return
	}

	select {
	case done <- err:
	default:
	}
}

func monitorSSHConnection(ctx context.Context, client *ssh.Client, done chan<- error) {
	go func() {
		if err := client.Wait(); err != nil && ctx.Err() == nil {
			notifyForwardDone(ctx, done, fmt.Errorf("ssh connection interrupted: %w", err))
		}
	}()

	go func() {
		ticker := time.NewTicker(keepaliveInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}

			result := make(chan error, 1)
			go func() {
				_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
				result <- err
			}()

			timer := time.NewTimer(keepaliveTimeout)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return
			case err := <-result:
				if !timer.Stop() {
					<-timer.C
				}
				if err != nil {
					notifyForwardDone(ctx, done, fmt.Errorf("ssh keepalive failed: %w", err))
					_ = client.Close()
					return
				}
			case <-timer.C:
				notifyForwardDone(ctx, done, fmt.Errorf("ssh keepalive timed out after %s", keepaliveTimeout))
				_ = client.Close()
				return
			}
		}
	}()
}

func emitForwardEvent(events chan<- ForwardEvent, status ForwardStatus, err error) {
	select {
	case events <- ForwardEvent{Status: status, Err: err}:
	default:
	}
}

func emitForwardRetryEvent(events chan<- ForwardEvent, remaining time.Duration) {
	select {
	case events <- ForwardEvent{Status: ForwardStatusReconnecting, RetryIn: remaining}:
	default:
	}
}

func emitManualRetryFailure(events chan<- ForwardEvent, err error) {
	select {
	case events <- ForwardEvent{
		Status:            ForwardStatusReconnecting,
		Err:               err,
		ManualRetryFailed: true,
	}:
	default:
	}
}

func waitBeforeReconnect(ctx context.Context, retry <-chan struct{}, notify func(time.Duration)) (bool, error) {
	remaining := reconnectInterval
	notify(remaining)

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-retry:
			notify(0)
			return true, nil
		case <-ticker.C:
			remaining -= time.Second
			if remaining <= 0 {
				notify(0)
				return false, nil
			}
			notify(remaining)
		}
	}
}

func forwardRemoteConnection(ctx context.Context, remoteConn net.Conn, localAddr string) {
	defer remoteConn.Close()

	dialer := net.Dialer{Timeout: forwardDialTimeout}
	localConn, err := dialer.DialContext(ctx, "tcp", localAddr)
	if err != nil {
		return
	}
	defer localConn.Close()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_, _ = io.Copy(localConn, remoteConn)
	}()

	go func() {
		defer wg.Done()
		_, _ = io.Copy(remoteConn, localConn)
	}()

	wg.Wait()
}

func forwardLocalConnection(client *ssh.Client, localConn net.Conn, remoteAddr string) {
	defer localConn.Close()

	remoteConn, err := client.Dial("tcp", remoteAddr)
	if err != nil {
		return
	}
	defer remoteConn.Close()

	copyBoth(localConn, remoteConn)
}

func handleSOCKS5(client *ssh.Client, localConn net.Conn) {
	defer localConn.Close()

	targetAddr, err := readSOCKS5Connect(localConn)
	if err != nil {
		return
	}

	remoteConn, err := client.Dial("tcp", targetAddr)
	if err != nil {
		_, _ = localConn.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer remoteConn.Close()

	_, _ = localConn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
	copyBoth(localConn, remoteConn)
}

func readSOCKS5Connect(conn net.Conn) (string, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(conn, header); err != nil {
		return "", err
	}
	if header[0] != 0x05 {
		return "", fmt.Errorf("unsupported socks version %d", header[0])
	}

	methods := make([]byte, int(header[1]))
	if _, err := io.ReadFull(conn, methods); err != nil {
		return "", err
	}
	if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
		return "", err
	}

	request := make([]byte, 4)
	if _, err := io.ReadFull(conn, request); err != nil {
		return "", err
	}
	if request[0] != 0x05 || request[1] != 0x01 {
		return "", fmt.Errorf("unsupported socks request")
	}

	host, err := readSOCKS5Host(conn, request[3])
	if err != nil {
		return "", err
	}

	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(conn, portBytes); err != nil {
		return "", err
	}

	return net.JoinHostPort(host, fmt.Sprintf("%d", binary.BigEndian.Uint16(portBytes))), nil
}

func readSOCKS5Host(conn net.Conn, addressType byte) (string, error) {
	switch addressType {
	case 0x01:
		addr := make([]byte, net.IPv4len)
		if _, err := io.ReadFull(conn, addr); err != nil {
			return "", err
		}
		return net.IP(addr).String(), nil
	case 0x03:
		length := make([]byte, 1)
		if _, err := io.ReadFull(conn, length); err != nil {
			return "", err
		}

		domain := make([]byte, int(length[0]))
		if _, err := io.ReadFull(conn, domain); err != nil {
			return "", err
		}
		return string(domain), nil
	case 0x04:
		addr := make([]byte, net.IPv6len)
		if _, err := io.ReadFull(conn, addr); err != nil {
			return "", err
		}
		return net.IP(addr).String(), nil
	default:
		return "", fmt.Errorf("unsupported socks address type %d", addressType)
	}
}

func copyBoth(left net.Conn, right net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_, _ = io.Copy(left, right)
	}()

	go func() {
		defer wg.Done()
		_, _ = io.Copy(right, left)
	}()

	wg.Wait()
}

func closeListeners(listeners []net.Listener) {
	for _, listener := range listeners {
		_ = listener.Close()
	}
}

func localForwardSummary(forwards []LocalForwardSpec, socksAddr string) string {
	parts := make([]string, 0, len(forwards)+1)
	for _, forward := range forwards {
		parts = append(parts, fmt.Sprintf("%s->%s", forward.LocalAddr, forward.RemoteAddr))
	}
	if socksAddr != "" {
		parts = append(parts, "SOCKS5 "+socksAddr)
	}

	return strings.Join(parts, ", ")
}

func forwardConnection(client *ssh.Client, localConn net.Conn, remoteAddr string) {
	defer localConn.Close()

	remoteConn, err := client.Dial("tcp", remoteAddr)
	if err != nil {
		return
	}
	defer remoteConn.Close()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_, _ = io.Copy(remoteConn, localConn)
	}()

	go func() {
		defer wg.Done()
		_, _ = io.Copy(localConn, remoteConn)
	}()

	wg.Wait()
}
