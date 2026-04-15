package playbook

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const BuiltinPlaybooksDir = "playbooks"

var builtinPlaybookFiles = []string{
	"check-health.yml",
	"cleanup-logs.yml",
	"collect-info.yml",
	"deploy-app.yml",
	"update-system.yml",
}

func BuiltinPlaybookFiles() []string {
	files := make([]string, len(builtinPlaybookFiles))
	copy(files, builtinPlaybookFiles)
	sort.Strings(files)
	return files
}

func BuiltinPlaybookPath(file string) string {
	return filepath.Join(BuiltinPlaybooksDir, file)
}

func GetBuiltinPlaybook(name string) (*Playbook, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return nil, fmt.Errorf("playbook 名称不能为空")
	}

	for _, file := range builtinPlaybookFiles {
		if file == trimmed {
			return Load(BuiltinPlaybookPath(file))
		}
	}

	return nil, fmt.Errorf("内置 playbook 不存在：%s", trimmed)
}

func Resolve(name string) (*Playbook, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return nil, fmt.Errorf("playbook 名称不能为空")
	}

	if p, err := GetBuiltinPlaybook(trimmed); err == nil {
		return p, nil
	}

	if p, err := GetBuiltinPlaybook(trimmed + ".yml"); err == nil {
		return p, nil
	}

	home, err := os.UserHomeDir()
	if err == nil {
		userPlaybook := filepath.Join(home, ".sshops", "playbooks", trimmed+".yml")
		if _, statErr := os.Stat(userPlaybook); statErr == nil {
			return Load(userPlaybook)
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return nil, statErr
		}
	}

	return Load(trimmed)
}
