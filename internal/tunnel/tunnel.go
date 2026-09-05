package tunnel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"golang.org/x/crypto/ssh"
)

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
