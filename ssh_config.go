package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

type SSHHost struct {
	Name     string
	HostName string
	User     string
	Port     int
}

type parsedSSHConfig struct {
	hosts map[string]*SSHHost
	order []string
}

// ParseSSHConfig returns concrete Host aliases in the order they first appear.
// Wildcard and negated patterns are omitted because they are not connectable
// aliases on their own.
func ParseSSHConfig(path string) ([]SSHHost, error) {
	expanded, err := expandUserPath(path)
	if err != nil {
		return nil, err
	}

	parser := &parsedSSHConfig{
		hosts: make(map[string]*SSHHost),
	}
	if err := parser.parseFile(expanded, true, make(map[string]bool)); err != nil {
		return nil, err
	}

	hosts := make([]SSHHost, 0, len(parser.order))
	for _, name := range parser.order {
		host := *parser.hosts[name]
		if host.HostName == "" {
			host.HostName = host.Name
		}
		if host.Port == 0 {
			host.Port = 22
		}
		hosts = append(hosts, host)
	}
	return hosts, nil
}

func (p *parsedSSHConfig) parseFile(path string, required bool, stack map[string]bool) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	absPath = filepath.Clean(absPath)
	if stack[absPath] {
		return nil
	}

	file, err := os.Open(absPath)
	if err != nil {
		if os.IsNotExist(err) && !required {
			return nil
		}
		return err
	}
	defer file.Close()

	stack[absPath] = true
	defer delete(stack, absPath)

	var activeHosts []*SSHHost
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	for scanner.Scan() {
		fields := splitSSHConfigLine(scanner.Text())
		if len(fields) == 0 {
			continue
		}

		keyword := strings.ToLower(fields[0])
		values := fields[1:]

		switch keyword {
		case "host":
			activeHosts = activeHosts[:0]
			for _, alias := range values {
				if !isConcreteSSHAlias(alias) {
					continue
				}
				activeHosts = append(activeHosts, p.ensureHost(alias))
			}
		case "match":
			activeHosts = nil
		case "include":
			for _, pattern := range values {
				if err := p.parseInclude(filepath.Dir(absPath), pattern, stack); err != nil {
					return err
				}
			}
		case "hostname":
			if value := firstSSHValue(values); value != "" {
				for _, host := range activeHosts {
					if host.HostName == "" {
						host.HostName = value
					}
				}
			}
		case "user":
			if value := firstSSHValue(values); value != "" {
				for _, host := range activeHosts {
					if host.User == "" {
						host.User = value
					}
				}
			}
		case "port":
			if value := firstSSHValue(values); value != "" {
				port, parseErr := strconv.Atoi(value)
				if parseErr == nil && port > 0 && port <= 65535 {
					for _, host := range activeHosts {
						if host.Port == 0 {
							host.Port = port
						}
					}
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read %s: %w", absPath, err)
	}
	return nil
}

func (p *parsedSSHConfig) parseInclude(baseDir, pattern string, stack map[string]bool) error {
	expanded, err := expandUserPath(pattern)
	if err != nil {
		return nil
	}
	if !filepath.IsAbs(expanded) {
		expanded = filepath.Join(baseDir, expanded)
	}

	matches, err := filepath.Glob(expanded)
	if err != nil {
		return nil
	}
	sort.Strings(matches)
	for _, match := range matches {
		if err := p.parseFile(match, false, stack); err != nil {
			return err
		}
	}
	return nil
}

func (p *parsedSSHConfig) ensureHost(alias string) *SSHHost {
	if host, ok := p.hosts[alias]; ok {
		return host
	}
	host := &SSHHost{Name: alias}
	p.hosts[alias] = host
	p.order = append(p.order, alias)
	return host
}

func firstSSHValue(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func isConcreteSSHAlias(alias string) bool {
	if alias == "" || strings.HasPrefix(alias, "-") {
		return false
	}
	return !strings.ContainsAny(alias, "*?[]!")
}

func splitSSHConfigLine(line string) []string {
	var fields []string
	var current strings.Builder
	var quote rune
	escaped := false

	flush := func() {
		if current.Len() > 0 {
			fields = append(fields, current.String())
			current.Reset()
		}
	}

	for _, r := range line {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				current.WriteRune(r)
			}
			continue
		}
		switch {
		case r == '"' || r == '\'':
			quote = r
		case r == '#':
			flush()
			return fields
		case unicode.IsSpace(r):
			flush()
		default:
			current.WriteRune(r)
		}
	}
	if escaped {
		current.WriteRune('\\')
	}
	flush()
	return fields
}

func expandUserPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" || path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "" || path == "~" {
			return home, nil
		}
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}
