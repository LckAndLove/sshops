package inventory

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Host struct {
	Name       string            `yaml:"name"`
	Host       string            `yaml:"host"`
	Port       int               `yaml:"port"`
	User       string            `yaml:"user"`
	KeyPath    string            `yaml:"key_path"`
	Tags       map[string]string `yaml:"tags"`
	Groups     []string          `yaml:"groups"`
	ProxyChain string            `yaml:"proxy_chain"`
}

type Inventory struct {
	Hosts []*Host `yaml:"hosts"`
	path  string
}

func Load(path string) (*Inventory, error) {
	inv := &Inventory{Hosts: make([]*Host, 0), path: path}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return inv, nil
		}
		return nil, err
	}

	if len(strings.TrimSpace(string(data))) == 0 {
		return inv, nil
	}

	if err := yaml.Unmarshal(data, inv); err != nil {
		return nil, err
	}
	if inv.Hosts == nil {
		inv.Hosts = make([]*Host, 0)
	}
	inv.path = path
	return inv, nil
}

func (inv *Inventory) Save() error {
	if inv == nil {
		return errors.New("inventory is nil")
	}
	if inv.path == "" {
		return errors.New("inventory path is empty")
	}

	if inv.Hosts == nil {
		inv.Hosts = make([]*Host, 0)
	}

	dir := filepath.Dir(inv.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	payload, err := yaml.Marshal(inv)
	if err != nil {
		return err
	}
	return os.WriteFile(inv.path, payload, 0o600)
}

func (inv *Inventory) Add(h *Host) error {
	if inv == nil {
		return errors.New("inventory is nil")
	}
	if h == nil {
		return errors.New("主机信息不能为空")
	}
	h.Name = strings.TrimSpace(h.Name)
	if h.Name == "" {
		return errors.New("主机名称不能为空")
	}

	for _, existing := range inv.Hosts {
		if existing != nil && existing.Name == h.Name {
			return fmt.Errorf("主机 %s 已存在，请使用不同的名称", h.Name)
		}
	}

	inv.Hosts = append(inv.Hosts, normalizeHost(h))
	return nil
}

func (inv *Inventory) Remove(name string) error {
	if inv == nil {
		return errors.New("inventory is nil")
	}
	name = strings.TrimSpace(name)
	for i, h := range inv.Hosts {
		if h != nil && h.Name == name {
			inv.Hosts = append(inv.Hosts[:i], inv.Hosts[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("主机 %s 不存在", name)
}

func (inv *Inventory) Get(name string) (*Host, error) {
	if inv == nil {
		return nil, errors.New("inventory is nil")
	}
	name = strings.TrimSpace(name)
	for _, h := range inv.Hosts {
		if h != nil && h.Name == name {
			return normalizeHost(h), nil
		}
	}
	return nil, fmt.Errorf("主机 %s 不存在", name)
}

func (inv *Inventory) List() []*Host {
	if inv == nil || len(inv.Hosts) == 0 {
		return []*Host{}
	}
	result := make([]*Host, 0, len(inv.Hosts))
	for _, h := range inv.Hosts {
		if h == nil {
			continue
		}
		result = append(result, normalizeHost(h))
	}
	return result
}

func normalizeHost(in *Host) *Host {
	if in == nil {
		return nil
	}

	out := *in
	if out.Port <= 0 {
		out.Port = 22
	}
	if strings.TrimSpace(out.User) == "" {
		out.User = "root"
	}
	if out.Tags == nil {
		out.Tags = map[string]string{}
	}
	if out.Groups == nil {
		out.Groups = []string{}
	}
	return &out
}
