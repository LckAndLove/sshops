package inventory

import (
	"fmt"
	"os"
	"strings"
)

func FilterByGroup(hosts []*Host, group string) []*Host {
	group = strings.TrimSpace(group)
	if group == "" {
		return hosts
	}

	filtered := make([]*Host, 0)
	for _, h := range hosts {
		if h == nil {
			continue
		}
		for _, g := range h.Groups {
			if strings.TrimSpace(g) == group {
				filtered = append(filtered, h)
				break
			}
		}
	}
	return filtered
}

func FilterByTags(hosts []*Host, tagStr string) []*Host {
	criteria := parseTagCriteria(tagStr)
	if len(criteria) == 0 {
		return hosts
	}

	filtered := make([]*Host, 0)
	for _, h := range hosts {
		if h == nil {
			continue
		}
		if matchesAllTags(h.Tags, criteria) {
			filtered = append(filtered, h)
		}
	}
	return filtered
}

func FilterByGroupAndTags(hosts []*Host, group, tagStr string) []*Host {
	result := hosts
	if strings.TrimSpace(group) != "" {
		result = FilterByGroup(result, group)
	}
	if strings.TrimSpace(tagStr) != "" {
		result = FilterByTags(result, tagStr)
	}
	return result
}

func parseTagCriteria(tagStr string) map[string]string {
	criteria := make(map[string]string)
	trimmed := strings.TrimSpace(tagStr)
	if trimmed == "" {
		return criteria
	}

	items := strings.Split(trimmed, ",")
	for _, item := range items {
		part := strings.TrimSpace(item)
		if part == "" {
			continue
		}

		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 || strings.TrimSpace(kv[0]) == "" || strings.TrimSpace(kv[1]) == "" {
			fmt.Fprintf(os.Stderr, "⚠ 标签格式无效，已忽略：%s\n", part)
			continue
		}

		key := strings.TrimSpace(kv[0])
		value := strings.TrimSpace(kv[1])
		criteria[key] = value
	}

	return criteria
}

func matchesAllTags(tags map[string]string, criteria map[string]string) bool {
	if len(criteria) == 0 {
		return true
	}
	if tags == nil {
		return false
	}
	for key, expected := range criteria {
		if actual, ok := tags[key]; !ok || actual != expected {
			return false
		}
	}
	return true
}
