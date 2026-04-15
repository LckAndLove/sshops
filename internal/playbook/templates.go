package playbook

import (
	"path/filepath"
	"sort"
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
