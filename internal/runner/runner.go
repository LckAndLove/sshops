package runner

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yourname/sshops/internal/audit"
	"github.com/yourname/sshops/internal/inventory"
	sshclient "github.com/yourname/sshops/internal/ssh"
)

type Task struct {
	Host     *inventory.Host
	Command  string
	KeyPath  string
	Password string
}

type Result struct {
	Host     *inventory.Host
	ExitCode int
	Error    error
	Duration time.Duration
	Output   string
}

type Runner struct {
	Concurrency int
	Timeout     int
	Retry       int
	RetryDelay  time.Duration
	Progress    *Display
	Audit       *audit.Logger
}

type indexedTask struct {
	idx  int
	task Task
}

func NewRunner(concurrency, timeout, retry int) *Runner {
	if concurrency <= 0 {
		concurrency = 10
	}
	if timeout <= 0 {
		timeout = 30
	}
	if retry < 0 {
		retry = 0
	}
	return &Runner{
		Concurrency: concurrency,
		Timeout:     timeout,
		Retry:       retry,
		RetryDelay:  2 * time.Second,
	}
}

func (r *Runner) Run(tasks []Task) []Result {
	results := make([]Result, len(tasks))
	if len(tasks) == 0 {
		return results
	}

	workerCount := r.Concurrency
	if workerCount > len(tasks) {
		workerCount = len(tasks)
	}

	jobs := make(chan indexedTask, len(tasks))
	for idx, t := range tasks {
		jobs <- indexedTask{idx: idx, task: t}
	}
	close(jobs)

	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				res := r.executeWithRetry(job.task)
				results[job.idx] = res
			}
		}()
	}

	wg.Wait()
	return results
}

func (r *Runner) executeWithRetry(task Task) Result {
	if task.Host == nil {
		return Result{Error: errors.New("主机信息为空")}
	}

	if r.Progress != nil {
		r.Progress.SetStatus(task.Host.Name, "running")
	}

	overallStart := time.Now()
	attempts := r.Retry + 1
	delay := r.RetryDelay
	if delay <= 0 {
		delay = 2 * time.Second
	}

	var final Result
	for attempt := 1; attempt <= attempts; attempt++ {
		res := r.runOnce(task)
		final = res
		if res.Error == nil && res.ExitCode == 0 {
			break
		}
		if attempt < attempts {
			time.Sleep(delay)
			delay *= 2
		}
	}

	final.Duration = time.Since(overallStart)
	if r.Progress != nil {
		if final.Error == nil && final.ExitCode == 0 {
			r.Progress.SetStatus(task.Host.Name, "ok", final.Duration)
		} else {
			r.Progress.SetStatus(task.Host.Name, "fail", final.Duration)
		}
	}

	if r.Audit != nil {
		_ = r.Audit.Log(&audit.Result{
			HostName: task.Host.Name,
			HostAddr: hostAddr(task.Host),
			ExitCode: final.ExitCode,
			Error:    final.Error,
			Duration: final.Duration,
		}, task.Command)
	}

	return final
}

func (r *Runner) runOnce(task Task) Result {
	res := Result{Host: task.Host, ExitCode: 1}
	if task.Host == nil {
		res.Error = errors.New("主机信息为空")
		return res
	}

	client := sshclient.NewClient(task.Host.Host, normalizePort(task.Host.Port), normalizeUser(task.Host.User), r.Timeout)
	proxyChain, proxyErr := parseProxyChain(task.Host.ProxyChain, normalizeUser(task.Host.User), task.KeyPath, task.Password)
	if proxyErr != nil {
		res.Error = proxyErr
		return res
	}
	client.Proxies = proxyChain

	if strings.TrimSpace(task.KeyPath) != "" {
		if err := client.WithKey(task.KeyPath); err != nil {
			res.Error = err
			return res
		}
	} else if strings.TrimSpace(task.Password) != "" {
		if err := client.WithPassword(task.Password); err != nil {
			res.Error = err
			return res
		}
	} else {
		res.Error = sshclient.ErrAuthFailed
		return res
	}

	if err := client.Connect(); err != nil {
		res.Error = err
		return res
	}

	combined := &safeBuffer{}
	done := make(chan struct{})
	start := time.Now()
	var exitCode int
	var runErr error
	go func() {
		exitCode, runErr = client.RunWithPrefixCapture(task.Command, task.Host.Name, combined, combined)
		close(done)
	}()

	select {
	case <-done:
		res.ExitCode = exitCode
		if runErr != nil {
			res.Error = runErr
		} else if exitCode != 0 {
			res.Error = fmt.Errorf("命令退出码 %d", exitCode)
		}
	case <-time.After(time.Duration(r.Timeout) * time.Second):
		client.CloseForce()
		res.ExitCode = 1
		res.Error = sshclient.ErrConnectTimeout
	}

	res.Duration = time.Since(start)
	res.Output = combined.String()
	return res
}

func normalizePort(port int) int {
	if port <= 0 {
		return 22
	}
	return port
}

func normalizeUser(user string) string {
	if strings.TrimSpace(user) == "" {
		return "root"
	}
	return strings.TrimSpace(user)
}

func hostAddr(h *inventory.Host) string {
	if h == nil {
		return ""
	}
	return h.Host + ":" + strconv.Itoa(normalizePort(h.Port))
}

func parseProxyChain(raw string, defaultUser string, keyPath string, password string) ([]sshclient.ProxyConfig, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}

	segments := strings.Split(trimmed, ",")
	proxies := make([]sshclient.ProxyConfig, 0, len(segments))
	for _, seg := range segments {
		item := strings.TrimSpace(seg)
		if item == "" {
			return nil, errors.New("跳板机参数格式错误：存在空项")
		}

		user := defaultUser
		hostPort := item
		if strings.Contains(item, "@") {
			parts := strings.SplitN(item, "@", 2)
			if strings.TrimSpace(parts[0]) != "" {
				user = strings.TrimSpace(parts[0])
			}
			hostPort = strings.TrimSpace(parts[1])
		}

		host, port, err := splitHostPortWithDefault(hostPort, 22)
		if err != nil {
			return nil, err
		}

		proxies = append(proxies, sshclient.ProxyConfig{
			Host:     host,
			Port:     port,
			User:     user,
			KeyPath:  keyPath,
			Password: password,
		})
	}
	return proxies, nil
}

func splitHostPortWithDefault(hostPort string, defaultPort int) (string, int, error) {
	value := strings.TrimSpace(hostPort)
	if value == "" {
		return "", 0, errors.New("invalid host")
	}
	if strings.Contains(value, ":") {
		idx := strings.LastIndex(value, ":")
		if idx <= 0 || idx >= len(value)-1 {
			return "", 0, errors.New("invalid host:port")
		}
		host := strings.TrimSpace(value[:idx])
		portText := strings.TrimSpace(value[idx+1:])
		port, err := strconv.Atoi(portText)
		if err != nil || port <= 0 {
			return "", 0, errors.New("invalid port")
		}
		return host, port, nil
	}
	return value, defaultPort, nil
}

type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}
