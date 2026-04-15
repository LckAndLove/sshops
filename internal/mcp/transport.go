package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync"
)

func RunStdio(server *Server) {
	scanner := bufio.NewScanner(os.Stdin)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req JSONRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			resp := &JSONRPCResponse{JSONRPC: "2.0", Error: &RPCError{Code: -32600, Message: "Invalid Request"}}
			_ = encoder.Encode(resp)
			continue
		}

		resp := server.Handle(&req)
		if resp == nil {
			continue
		}
		if err := encoder.Encode(resp); err != nil {
			fmt.Fprintf(os.Stderr, "MCP stdio 响应写入失败: %v\n", err)
			return
		}
	}
	if err := scanner.Err(); err != nil && err != io.EOF {
		fmt.Fprintf(os.Stderr, "MCP stdio 读取失败: %v\n", err)
	}
}

func RunSSE(server *Server, port int) {
	if port <= 0 {
		port = 3000
	}

	type clientStream struct {
		ch chan string
	}

	var (
		mu      sync.Mutex
		clients = map[int]*clientStream{}
		nextID  = 1
	)

	http.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "stream unsupported", http.StatusInternalServerError)
			return
		}

		stream := &clientStream{ch: make(chan string, 32)}
		mu.Lock()
		id := nextID
		nextID++
		clients[id] = stream
		mu.Unlock()

		defer func() {
			mu.Lock()
			delete(clients, id)
			close(stream.ch)
			mu.Unlock()
		}()

		fmt.Fprintf(w, "data: {\"endpoint\":\"/message\"}\n\n")
		flusher.Flush()

		notify := r.Context().Done()
		for {
			select {
			case <-notify:
				return
			case msg, ok := <-stream.ch:
				if !ok {
					return
				}
				fmt.Fprintf(w, "event: message\ndata: %s\n\n", msg)
				flusher.Flush()
			}
		}
	})

	http.HandleFunc("/message", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req JSONRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}

		resp := server.Handle(&req)
		if resp == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		payload, err := json.Marshal(resp)
		if err != nil {
			http.Error(w, "marshal failed", http.StatusInternalServerError)
			return
		}

		mu.Lock()
		for _, c := range clients {
			select {
			case c.ch <- string(payload):
			default:
			}
		}
		mu.Unlock()

		w.WriteHeader(http.StatusAccepted)
	})

	fmt.Fprintf(os.Stderr, "MCP SSE server 已启动，监听端口 %d\n", port)
	fmt.Fprintln(os.Stderr, "Claude Desktop 配置：")
	fmt.Fprintf(os.Stderr, "  {\"mcpServers\":{\"sshops\":{\"url\":\"http://localhost:%d/sse\"}}}\n", port)
	if err := http.ListenAndServe(":"+strconv.Itoa(port), nil); err != nil {
		fmt.Fprintf(os.Stderr, "MCP SSE server 退出: %v\n", err)
	}
}
