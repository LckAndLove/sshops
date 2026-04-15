package playbook

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"text/template"
	"time"

	"gopkg.in/yaml.v3"
)

type Playbook struct {
	Name  string            `yaml:"name"`
	Hosts string            `yaml:"hosts"`
	Vars  map[string]string `yaml:"vars"`
	Tasks []Task            `yaml:"tasks"`
}

type Task struct {
	Name        string `yaml:"name"`
	Command     string `yaml:"command"`
	When        string `yaml:"when"`
	Register    string `yaml:"register"`
	IgnoreError bool   `yaml:"ignore_error"`
}

type TaskResult struct {
	TaskName string
	Host     string
	ExitCode int
	Output   string
	Error    error
	Duration time.Duration
}

func Load(path string) (*Playbook, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("Playbook 文件不存在：%s", path)
		}
		return nil, err
	}

	p := &Playbook{}
	if err := yaml.Unmarshal(data, p); err != nil {
		return nil, fmt.Errorf("解析 Playbook YAML 失败: %w", err)
	}

	if p.Vars == nil {
		p.Vars = map[string]string{}
	}
	if p.Tasks == nil {
		p.Tasks = []Task{}
	}

	for i := range p.Tasks {
		t := p.Tasks[i]
		if _, err := template.New(fmt.Sprintf("task_%d_%s", i, t.Name)).Parse(t.Command); err != nil {
			return nil, fmt.Errorf("任务 %q 的 command 模板无效: %w", t.Name, err)
		}
	}

	return p, nil
}

func Render(p *Playbook, task *Task, vars map[string]string) (string, error) {
	if p == nil {
		return "", errors.New("playbook 不能为空")
	}
	if task == nil {
		return "", errors.New("task 不能为空")
	}

	finalVars := map[string]string{}
	for k, v := range p.Vars {
		finalVars[k] = v
	}
	for k, v := range vars {
		finalVars[k] = v
	}

	tpl, err := template.New(task.Name).Option("missingkey=error").Parse(task.Command)
	if err != nil {
		return "", fmt.Errorf("解析任务模板失败: %w", err)
	}

	var out bytes.Buffer
	if err := tpl.Execute(&out, map[string]any{"vars": finalVars}); err != nil {
		return "", fmt.Errorf("渲染任务模板失败: %w", err)
	}

	return out.String(), nil
}
