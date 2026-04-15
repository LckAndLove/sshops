package ssh

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

var (
	ErrPrivateKeyNotFound = errors.New("private key not found")
	ErrInvalidPrivateKey  = errors.New("invalid private key")
	ErrAuthFailed         = errors.New("authentication failed")
	ErrConnectTimeout     = errors.New("connect timeout")
	ErrConnectionRefused  = errors.New("connection refused")
	ErrConnectFailed      = errors.New("connect failed")
	ErrCommandRunFailed   = errors.New("command run failed")
)

type Client struct {
	Host    string
	Port    int
	User    string
	Timeout int
	client  *gossh.Client
	auth    []gossh.AuthMethod
}

func NewClient(host string, port int, user string, timeout int) *Client {
	return &Client{
		Host:    host,
		Port:    port,
		User:    user,
		Timeout: timeout,
	}
}

func (c *Client) WithKey(keyPath string) error {
	privateKey, err := os.ReadFile(keyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrPrivateKeyNotFound
		}
		return ErrInvalidPrivateKey
	}

	signer, err := gossh.ParsePrivateKey(privateKey)
	if err != nil {
		var passErr *gossh.PassphraseMissingError
		if errors.As(err, &passErr) {
			fmt.Print("请输入私钥口令: ")
			passphrase, readErr := term.ReadPassword(int(syscall.Stdin))
			fmt.Println()
			if readErr != nil {
				return ErrInvalidPrivateKey
			}
			signer, err = gossh.ParsePrivateKeyWithPassphrase(privateKey, passphrase)
			if err != nil {
				return ErrInvalidPrivateKey
			}
		} else {
			return ErrInvalidPrivateKey
		}
	}

	c.auth = []gossh.AuthMethod{gossh.PublicKeys(signer)}
	return nil
}

func (c *Client) WithPassword(password string) error {
	if strings.TrimSpace(password) == "" {
		return ErrAuthFailed
	}
	c.auth = []gossh.AuthMethod{gossh.Password(password)}
	return nil
}

func (c *Client) Connect() error {
	if len(c.auth) == 0 {
		return ErrAuthFailed
	}

	addr := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	sshConfig := &gossh.ClientConfig{
		User:            c.User,
		Auth:            c.auth,
		Timeout:         time.Duration(c.Timeout) * time.Second,
		HostKeyCallback: gossh.InsecureIgnoreHostKey(), // TODO: 生产环境需验证 host key。
	}

	client, err := gossh.Dial("tcp", addr, sshConfig)
	if err != nil {
		msg := strings.ToLower(err.Error())
		switch {
		case strings.Contains(msg, "unable to authenticate"):
			return ErrAuthFailed
		case strings.Contains(msg, "i/o timeout") || strings.Contains(msg, "connection timed out"):
			return ErrConnectTimeout
		case strings.Contains(msg, "connection refused"):
			return ErrConnectionRefused
		default:
			return ErrConnectFailed
		}
	}

	c.client = client
	return nil
}

func (c *Client) Run(command string) (exitCode int, err error) {
	if c.client == nil {
		return 1, ErrCommandRunFailed
	}
	defer c.Close()

	session, err := c.client.NewSession()
	if err != nil {
		return 1, ErrCommandRunFailed
	}
	defer session.Close()

	session.Stdout = os.Stdout
	session.Stderr = os.Stderr

	err = session.Run(command)
	if err == nil {
		return 0, nil
	}

	var exitErr *gossh.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitStatus(), nil
	}

	return 1, ErrCommandRunFailed
}

func (c *Client) Close() {
	if c.client != nil {
		_ = c.client.Close()
		c.client = nil
	}
}
