package ssh

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

type ProxyConfig struct {
	Host     string
	Port     int
	User     string
	KeyPath  string
	Password string
}

type ProxyHopError struct {
	Hop    int
	Node   string
	Reason error
}

func (e *ProxyHopError) Error() string {
	if e == nil {
		return "proxy hop failed"
	}
	return fmt.Sprintf("proxy hop %d (%s) failed: %v", e.Hop, e.Node, e.Reason)
}

func (e *ProxyHopError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Reason
}

func DialWithProxy(
	proxies []ProxyConfig,
	target string,
	targetPort int,
	targetUser string,
	authMethods []gossh.AuthMethod,
	timeout int,
) (*gossh.Client, error) {
	if len(authMethods) == 0 {
		return nil, ErrAuthFailed
	}

	if timeout <= 0 {
		timeout = 30
	}

	targetAddr := net.JoinHostPort(target, strconv.Itoa(targetPort))
	targetConfig := &gossh.ClientConfig{
		User:            targetUser,
		Auth:            authMethods,
		Timeout:         time.Duration(timeout) * time.Second,
		HostKeyCallback: gossh.InsecureIgnoreHostKey(), // TODO: 生产环境需验证 host key。
	}

	if len(proxies) == 0 {
		client, err := gossh.Dial("tcp", targetAddr, targetConfig)
		if err != nil {
			return nil, classifyDialError(err)
		}
		return client, nil
	}

	first := proxies[0]
	firstAddr := net.JoinHostPort(first.Host, strconv.Itoa(first.Port))
	firstConfig := &gossh.ClientConfig{
		User:            first.User,
		Auth:            authMethods,
		Timeout:         time.Duration(timeout) * time.Second,
		HostKeyCallback: gossh.InsecureIgnoreHostKey(), // TODO: 生产环境需验证 host key。
	}

	current, err := gossh.Dial("tcp", firstAddr, firstConfig)
	if err != nil {
		return nil, &ProxyHopError{
			Hop:    1,
			Node:   fmt.Sprintf("%s@%s:%d", first.User, first.Host, first.Port),
			Reason: classifyDialError(err),
		}
	}

	for i := 1; i < len(proxies); i++ {
		next := proxies[i]
		nextAddr := net.JoinHostPort(next.Host, strconv.Itoa(next.Port))
		nextConfig := &gossh.ClientConfig{
			User:            next.User,
			Auth:            authMethods,
			Timeout:         time.Duration(timeout) * time.Second,
			HostKeyCallback: gossh.InsecureIgnoreHostKey(), // TODO: 生产环境需验证 host key。
		}

		netConn, dialErr := current.Dial("tcp", nextAddr)
		if dialErr != nil {
			_ = current.Close()
			return nil, &ProxyHopError{
				Hop:    i + 1,
				Node:   fmt.Sprintf("%s@%s:%d", next.User, next.Host, next.Port),
				Reason: classifyDialError(dialErr),
			}
		}

		conn, chans, reqs, newConnErr := gossh.NewClientConn(netConn, nextAddr, nextConfig)
		if newConnErr != nil {
			_ = netConn.Close()
			_ = current.Close()
			return nil, &ProxyHopError{
				Hop:    i + 1,
				Node:   fmt.Sprintf("%s@%s:%d", next.User, next.Host, next.Port),
				Reason: classifyDialError(newConnErr),
			}
		}

		prev := current
		current = gossh.NewClient(conn, chans, reqs)
		_ = prev.Close()
	}

	conn, chans, reqs, err := dialTargetThroughClient(current, targetAddr, targetConfig)
	if err != nil {
		_ = current.Close()
		return nil, &ProxyHopError{
			Hop:    len(proxies) + 1,
			Node:   fmt.Sprintf("%s@%s:%d", targetUser, target, targetPort),
			Reason: classifyDialError(err),
		}
	}

	client := gossh.NewClient(conn, chans, reqs)
	_ = current.Close()
	return client, nil
}

func dialTargetThroughClient(current *gossh.Client, targetAddr string, cfg *gossh.ClientConfig) (gossh.Conn, <-chan gossh.NewChannel, <-chan *gossh.Request, error) {
	netConn, err := current.Dial("tcp", targetAddr)
	if err != nil {
		return nil, nil, nil, err
	}

	conn, chans, reqs, err := gossh.NewClientConn(netConn, targetAddr, cfg)
	if err != nil {
		_ = netConn.Close()
		return nil, nil, nil, err
	}

	return conn, chans, reqs, nil
}

func classifyDialError(err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "unable to authenticate"):
		return ErrAuthFailed
	case strings.Contains(msg, "i/o timeout"), strings.Contains(msg, "connection timed out"):
		return ErrConnectTimeout
	case strings.Contains(msg, "connection refused"):
		return ErrConnectionRefused
	default:
		return ErrConnectFailed
	}
}
