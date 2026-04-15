package ssh

import (
	"sync"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

type ConnPool struct {
	mu      sync.Mutex
	conns   map[string]*poolEntry
	maxIdle int
}

type poolEntry struct {
	client   *gossh.Client
	lastUsed time.Time
	inUse    bool
}

var GlobalPool = NewConnPool(10)

func init() {
	GlobalPool.StartReaper(time.Minute)
}

func NewConnPool(maxIdle int) *ConnPool {
	if maxIdle <= 0 {
		maxIdle = 10
	}
	return &ConnPool{
		conns:   make(map[string]*poolEntry),
		maxIdle: maxIdle,
	}
}

func (p *ConnPool) Get(key string) *gossh.Client {
	p.mu.Lock()
	defer p.mu.Unlock()

	entry, ok := p.conns[key]
	if !ok || entry == nil || entry.client == nil || entry.inUse {
		return nil
	}

	entry.inUse = true
	entry.lastUsed = time.Now()
	return entry.client
}

func (p *ConnPool) Put(key string, client *gossh.Client) {
	if client == nil || key == "" {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if entry, ok := p.conns[key]; ok && entry != nil {
		entry.client = client
		entry.inUse = false
		entry.lastUsed = time.Now()
		return
	}

	if len(p.conns) >= p.maxIdle {
		oldestKey := ""
		var oldestTime time.Time
		for k, v := range p.conns {
			if v == nil || v.inUse {
				continue
			}
			if oldestKey == "" || v.lastUsed.Before(oldestTime) {
				oldestKey = k
				oldestTime = v.lastUsed
			}
		}
		if oldestKey != "" {
			if oldEntry := p.conns[oldestKey]; oldEntry != nil && oldEntry.client != nil {
				_ = oldEntry.client.Close()
			}
			delete(p.conns, oldestKey)
		}
	}

	p.conns[key] = &poolEntry{
		client:   client,
		lastUsed: time.Now(),
		inUse:    false,
	}
}

func (p *ConnPool) Remove(key string) {
	if key == "" {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	entry, ok := p.conns[key]
	if !ok {
		return
	}
	if entry != nil && entry.client != nil {
		_ = entry.client.Close()
	}
	delete(p.conns, key)
}

func (p *ConnPool) StartReaper(interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			p.reapIdle(5 * time.Minute)
		}
	}()
}

func (p *ConnPool) reapIdle(maxIdleTime time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	for key, entry := range p.conns {
		if entry == nil || entry.inUse {
			continue
		}
		if now.Sub(entry.lastUsed) > maxIdleTime {
			if entry.client != nil {
				_ = entry.client.Close()
			}
			delete(p.conns, key)
		}
	}
}
