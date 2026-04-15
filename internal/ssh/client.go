package ssh

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/fatih/color"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

const (
	maxOutputBytes = 10 * 1024 * 1024
	truncationNote = "[输出已截断，超过 10MB 限制]"
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

var streamPrintMu sync.Mutex

type Client struct {
	Host    string
	Port    int
	User    string
	Timeout int
	Proxies []ProxyConfig
	poolKey string

	client  *gossh.Client
	session *gossh.Session
	auth    []gossh.AuthMethod
}

type outputLimiter struct {
	mu           sync.Mutex
	written      int
	truncated    bool
	noticeEmited bool
	writers      []io.Writer
}

func newOutputLimiter(writers ...io.Writer) *outputLimiter {
	unique := make([]io.Writer, 0, len(writers))
	seen := map[io.Writer]bool{}
	for _, w := range writers {
		if w == nil || seen[w] {
			continue
		}
		seen[w] = true
		unique = append(unique, w)
	}
	return &outputLimiter{writers: unique}
}

func (l *outputLimiter) allow(text string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.truncated {
		return false
	}
	if l.written+len(text) > maxOutputBytes {
		l.truncated = true
		return false
	}
	l.written += len(text)
	return true
}

func (l *outputLimiter) emitNotice(prefix string) {
	l.mu.Lock()
	if l.noticeEmited {
		l.mu.Unlock()
		return
	}
	l.noticeEmited = true
	writers := append([]io.Writer{}, l.writers...)
	l.mu.Unlock()

	printLine(truncationNote, false, prefix)
	for _, w := range writers {
		if w == nil {
			continue
		}
		_, _ = io.WriteString(w, truncationNote+"\n")
	}
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

	c.poolKey = fmt.Sprintf("%s@%s:%d", c.User, c.Host, c.Port)
	if pooled := GlobalPool.Get(c.poolKey); pooled != nil {
		c.client = pooled
		return nil
	}

	client, err := DialWithProxy(c.Proxies, c.Host, c.Port, c.User, c.auth, c.Timeout)
	if err != nil {
		GlobalPool.Remove(c.poolKey)
		return err
	}

	c.client = client
	return nil
}

func (c *Client) Run(command string) (int, error) {
	return c.run(command, "", nil, nil)
}

func (c *Client) RunWithPrefix(command string, prefix string) (int, error) {
	return c.run(command, prefix, nil, nil)
}

func (c *Client) RunWithPrefixCapture(command string, prefix string, stdoutWriter io.Writer, stderrWriter io.Writer) (int, error) {
	return c.run(command, prefix, stdoutWriter, stderrWriter)
}

func (c *Client) run(command string, prefix string, stdoutWriter io.Writer, stderrWriter io.Writer) (exitCode int, err error) {
	if c.client == nil {
		return 1, ErrCommandRunFailed
	}
	if strings.TrimSpace(command) == "" {
		return 1, ErrCommandRunFailed
	}

	session, err := c.client.NewSession()
	if err != nil {
		c.CloseForce()
		return 1, ErrCommandRunFailed
	}
	c.session = session

	stdoutPipe, err := session.StdoutPipe()
	if err != nil {
		c.CloseForce()
		return 1, ErrCommandRunFailed
	}
	stderrPipe, err := session.StderrPipe()
	if err != nil {
		c.CloseForce()
		return 1, ErrCommandRunFailed
	}

	if err := session.Start(command); err != nil {
		c.CloseForce()
		return 1, ErrCommandRunFailed
	}

	limiter := newOutputLimiter(stdoutWriter, stderrWriter)
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		streamLines(stdoutPipe, false, prefix, stdoutWriter, limiter)
	}()

	go func() {
		defer wg.Done()
		streamLines(stderrPipe, true, prefix, stderrWriter, limiter)
	}()

	wg.Wait()
	waitErr := session.Wait()

	_ = c.Close()

	if waitErr == nil {
		return 0, nil
	}

	var exitErr *gossh.ExitError
	if errors.As(waitErr, &exitErr) {
		return exitErr.ExitStatus(), nil
	}

	c.CloseForce()
	return 1, ErrCommandRunFailed
}

func (c *Client) Close() error {
	if c.session != nil {
		_ = c.session.Close()
		c.session = nil
	}
	if c.client != nil && c.poolKey != "" {
		GlobalPool.Put(c.poolKey, c.client)
	}
	return nil
}

func (c *Client) CloseForce() {
	if c.session != nil {
		_ = c.session.Close()
		c.session = nil
	}
	if c.client != nil {
		_ = c.client.Close()
	}
	if c.poolKey != "" {
		GlobalPool.Remove(c.poolKey)
	}
	c.client = nil
}

func (c *Client) Raw() *gossh.Client {
	return c.client
}

func streamLines(reader io.Reader, isStderr bool, prefix string, extraWriter io.Writer, limiter *outputLimiter) {
	scanner := bufio.NewScanner(reader)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := normalizeOutput(scanner.Text())
		chunk := line + "\n"
		if limiter != nil && !limiter.allow(chunk) {
			limiter.emitNotice(prefix)
			return
		}
		printLine(line, isStderr, prefix)
		if extraWriter != nil {
			_, _ = io.WriteString(extraWriter, chunk)
		}
	}
}

func normalizeOutput(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "")
	return strings.ToValidUTF8(s, "?")
}

func printLine(line string, isStderr bool, prefix string) {
	streamPrintMu.Lock()
	defer streamPrintMu.Unlock()

	white := color.New(color.FgWhite)
	yellow := color.New(color.FgYellow)
	cyan := color.New(color.FgCyan)

	if prefix == "" {
		if isStderr {
			yellow.Fprintln(color.Output, line)
			return
		}
		white.Fprintln(color.Output, line)
		return
	}

	prefixText := "[" + prefix + "] "
	if isStderr {
		yellow.Fprint(color.Output, prefixText)
		yellow.Fprintln(color.Output, line)
		return
	}

	cyan.Fprint(color.Output, prefixText)
	white.Fprintln(color.Output, line)
}

func IsProxyHopError(err error) (*ProxyHopError, bool) {
	var hopErr *ProxyHopError
	if errors.As(err, &hopErr) {
		return hopErr, true
	}
	return nil, false
}

func BuildAddr(host string, port int) string {
	return host + ":" + strconv.Itoa(port)
}
