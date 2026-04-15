package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/yourname/sshops/internal/audit"
	"github.com/yourname/sshops/internal/config"
	"github.com/yourname/sshops/internal/inventory"
	"github.com/yourname/sshops/internal/playbook"
	execrunner "github.com/yourname/sshops/internal/runner"
	"github.com/yourname/sshops/internal/vault"
)

// WebServer provides a minimal HTTP API using only net/http.
type WebServer struct {
	inventory *inventory.Inventory
	vault     *vault.Vault
	config    *config.Config
	audit     *audit.Logger
	password  string
}

// NewWebServer creates a WebServer with required dependencies.
func NewWebServer(inv *inventory.Inventory, vault *vault.Vault, cfg *config.Config, audit *audit.Logger, password string) *WebServer {
	if cfg == nil {
		cfg = config.Default()
	}
	return &WebServer{
		inventory: inv,
		vault:     vault,
		config:    cfg,
		audit:     audit,
		password:  strings.TrimSpace(password),
	}
}

// Start starts the HTTP server and blocks.
func (s *WebServer) Start(port int) error {
	if port <= 0 {
		port = 8080
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/api/hosts", s.handleHosts)
	mux.HandleFunc("/api/hosts/", s.handleHostByName)
	mux.HandleFunc("/api/exec", s.handleExec)
	mux.HandleFunc("/api/metrics/", s.handleMetricsByName)
	mux.HandleFunc("/api/logs", s.handleLogs)
	mux.HandleFunc("/api/playbooks", s.handlePlaybooks)
	mux.HandleFunc("/api/playbooks/run", s.handleRunPlaybook)

	addr := ":" + strconv.Itoa(port)
	return http.ListenAndServe(addr, s.withMiddleware(mux))
}

func (s *WebServer) withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.applyCORS(w)

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		if strings.HasPrefix(r.URL.Path, "/api/") && s.password != "" {
			expected := "Bearer " + s.password
			if strings.TrimSpace(r.Header.Get("Authorization")) != expected {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

func (s *WebServer) applyCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
}

func (s *WebServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *WebServer) handleHosts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if s.inventory == nil {
		writeJSON(w, http.StatusOK, []*inventory.Host{})
		return
	}
	writeJSON(w, http.StatusOK, s.inventory.List())
}

func (s *WebServer) handleHostByName(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if s.inventory == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "host not found"})
		return
	}
	name := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/hosts/"))
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "host name is required"})
		return
	}
	h, err := s.inventory.Get(name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, h)
}

type execRequest struct {
	Host    string `json:"host"`
	Group   string `json:"group"`
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
}

func (s *WebServer) handleExec(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if s.inventory == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "inventory not initialized"})
		return
	}

	var req execRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}

	req.Command = strings.TrimSpace(req.Command)
	if req.Command == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "command is required"})
		return
	}

	hosts, err := s.selectExecHosts(req.Host, req.Group)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if len(hosts) == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no hosts matched"})
		return
	}

	timeout := req.Timeout
	if timeout <= 0 {
		timeout = s.config.ConnectTimeout
	}
	if timeout <= 0 {
		timeout = 30
	}

	tasks := make([]execrunner.Task, 0, len(hosts))
	for _, h := range hosts {
		if h == nil {
			continue
		}
		keyPath, password := s.resolveCredential(h)
		tasks = append(tasks, execrunner.Task{
			Host:     h,
			Command:  req.Command,
			KeyPath:  keyPath,
			Password: password,
		})
	}

	runner := execrunner.NewRunner(10, timeout, 0)
	runner.Audit = s.audit
	results := runner.Run(tasks)

	type execResult struct {
		Host     string `json:"host"`
		Address  string `json:"address"`
		ExitCode int    `json:"exit_code"`
		Duration string `json:"duration"`
		Output   string `json:"output,omitempty"`
		Error    string `json:"error,omitempty"`
	}

	resp := struct {
		Results []execResult `json:"results"`
	}{Results: make([]execResult, 0, len(results))}

	for _, rs := range results {
		item := execResult{ExitCode: rs.ExitCode, Duration: rs.Duration.Round(time.Millisecond).String(), Output: strings.TrimSpace(rs.Output)}
		if rs.Host != nil {
			item.Host = rs.Host.Name
			item.Address = rs.Host.Host
		}
		if rs.Error != nil {
			item.Error = rs.Error.Error()
		}
		resp.Results = append(resp.Results, item)
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *WebServer) selectExecHosts(hostName, groupName string) ([]*inventory.Host, error) {
	hostName = strings.TrimSpace(hostName)
	groupName = strings.TrimSpace(groupName)

	if hostName == "" && groupName == "" {
		return nil, errors.New("host or group is required")
	}

	if hostName != "" {
		h, err := s.inventory.Get(hostName)
		if err != nil {
			return nil, err
		}
		return []*inventory.Host{h}, nil
	}

	return inventory.FilterByGroup(s.inventory.List(), groupName), nil
}

func (s *WebServer) handleMetricsByName(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if s.inventory == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "inventory not initialized"})
		return
	}

	name := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/metrics/"))
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "host name is required"})
		return
	}
	h, err := s.inventory.Get(name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	timeout := s.config.ConnectTimeout
	if timeout <= 0 {
		timeout = 30
	}

	type metricCmd struct {
		name string
		cmd  string
	}
	cmds := []metricCmd{
		{name: "cpu", cmd: "top -bn1 | grep \"Cpu(s)\" | awk '{print $2}'"},
		{name: "memory", cmd: "free -h | awk '/^Mem:/{print $2,$3,$4}'"},
		{name: "disk", cmd: "df -h | grep -v tmpfs"},
		{name: "load", cmd: "uptime"},
		{name: "processes", cmd: "ps aux | wc -l"},
	}

	metrics := map[string]string{}
	for _, mc := range cmds {
		out, err := s.execSingleHost(h, mc.cmd, timeout)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		metrics[mc.name] = strings.TrimSpace(out)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"host":    h.Name,
		"address": h.Host,
		"metrics": metrics,
	})
}

func (s *WebServer) execSingleHost(h *inventory.Host, command string, timeout int) (string, error) {
	if h == nil {
		return "", errors.New("host is nil")
	}
	keyPath, password := s.resolveCredential(h)
	runner := execrunner.NewRunner(1, timeout, 0)
	runner.Audit = s.audit
	results := runner.Run([]execrunner.Task{{
		Host:     h,
		Command:  command,
		KeyPath:  keyPath,
		Password: password,
	}})
	if len(results) == 0 {
		return "", errors.New("no result")
	}
	res := results[0]
	if res.Error != nil {
		return strings.TrimSpace(res.Output), res.Error
	}
	return strings.TrimSpace(res.Output), nil
}

func (s *WebServer) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if s.audit == nil {
		writeJSON(w, http.StatusOK, []audit.LogEntry{})
		return
	}

	limit := 20
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid limit"})
			return
		}
		limit = n
	}

	logs, err := s.audit.Query(limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, logs)
}

func (s *WebServer) handlePlaybooks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"playbooks": playbook.BuiltinPlaybookFiles(),
	})
}

type runPlaybookRequest struct {
	Name string            `json:"name"`
	Vars map[string]string `json:"vars"`
}

func (s *WebServer) handleRunPlaybook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if s.inventory == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "inventory not initialized"})
		return
	}

	var req runPlaybookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if req.Vars == nil {
		req.Vars = map[string]string{}
	}

	pb, err := playbook.Resolve(req.Name)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	timeout := s.config.ConnectTimeout
	if timeout <= 0 {
		timeout = 30
	}
	pr := playbook.NewPlaybookRunner(s.inventory, execrunner.NewRunner(10, timeout, 0))
	pr.KeyPath = strings.TrimSpace(s.config.DefaultKeyPath)
	pr.Vars = req.Vars

	var out bytes.Buffer
	pr.Out = &out
	if err := pr.Run(pb); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"error":  err.Error(),
			"output": strings.TrimSpace(out.String()),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"output": strings.TrimSpace(out.String()),
	})
}

func (s *WebServer) resolveCredential(h *inventory.Host) (string, string) {
	keyPath := ""
	password := ""

	if s.vault != nil && h != nil {
		if cred, err := s.vault.Get(h.Name); err == nil && cred != nil {
			if strings.TrimSpace(cred.KeyPath) != "" {
				keyPath = strings.TrimSpace(cred.KeyPath)
			}
			if strings.TrimSpace(cred.Password) != "" {
				password = strings.TrimSpace(cred.Password)
			}
		}
	}

	if keyPath == "" && h != nil && strings.TrimSpace(h.KeyPath) != "" {
		keyPath = strings.TrimSpace(h.KeyPath)
	}
	if keyPath == "" && s.config != nil {
		keyPath = strings.TrimSpace(s.config.DefaultKeyPath)
	}
	return keyPath, password
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		http.Error(w, fmt.Sprintf("json encode error: %v", err), http.StatusInternalServerError)
	}
}
