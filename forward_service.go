package main

import (
	"fmt"
	"os"
	"strings"
)

type HostView struct {
	Name         string `json:"name"`
	HostName     string `json:"hostName"`
	User         string `json:"user"`
	Port         int    `json:"port"`
	Forwarding   bool   `json:"forwarding"`
	ForwardCount int    `json:"forwardCount"`
	ActiveCount  int    `json:"activeCount"`
}

type HostsSnapshot struct {
	ConfigPath string     `json:"configPath"`
	Hosts      []HostView `json:"hosts"`
	Error      string     `json:"error,omitempty"`
}

type ForwardService struct {
	configPath string
	manager    *ForwardManager
}

func NewForwardService(configPath string, manager *ForwardManager) *ForwardService {
	return &ForwardService{
		configPath: configPath,
		manager:    manager,
	}
}

func (s *ForwardService) GetHosts() HostsSnapshot {
	snapshot := HostsSnapshot{
		ConfigPath: s.configPath,
		Hosts:      make([]HostView, 0),
		Error:      s.manager.StorageError(),
	}

	hosts, err := ParseSSHConfig(s.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			snapshot.Error = fmt.Sprintf("未找到 SSH 配置文件：%s", s.configPath)
		} else {
			snapshot.Error = fmt.Sprintf("读取 SSH 配置失败：%v", err)
		}
		return snapshot
	}

	configured, active := s.manager.CountsByHost()
	for _, host := range hosts {
		activeCount := active[host.Name]
		snapshot.Hosts = append(snapshot.Hosts, HostView{
			Name:         host.Name,
			HostName:     host.HostName,
			User:         host.User,
			Port:         host.Port,
			Forwarding:   activeCount > 0,
			ForwardCount: configured[host.Name],
			ActiveCount:  activeCount,
		})
	}
	return snapshot
}

func (s *ForwardService) GetForwards(host string) ([]ForwardView, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil, fmt.Errorf("Host 不能为空")
	}
	return s.manager.ListForHost(host), nil
}

func (s *ForwardService) AddForward(input ForwardInput) (ForwardView, error) {
	if !s.hostExists(input.Host) {
		return ForwardView{}, fmt.Errorf("Host %q 不在 SSH 配置中", input.Host)
	}
	view, err := s.manager.Add(input)
	if err != nil {
		return ForwardView{}, err
	}
	if input.StartNow {
		started, startErr := s.manager.Start(view.ID)
		if startErr != nil {
			return started, fmt.Errorf("转发已保存，但启动失败：%w", startErr)
		}
		return started, nil
	}
	return view, nil
}

func (s *ForwardService) StartForward(id string) (ForwardView, error) {
	return s.manager.Start(id)
}

func (s *ForwardService) StopForward(id string) (ForwardView, error) {
	return s.manager.Stop(id)
}

func (s *ForwardService) DeleteForward(id string) error {
	return s.manager.Delete(id)
}

func (s *ForwardService) AutoStartConfigured() {
	hosts, err := ParseSSHConfig(s.configPath)
	if err != nil {
		return
	}
	allowed := make(map[string]bool, len(hosts))
	for _, host := range hosts {
		allowed[host.Name] = true
	}
	s.manager.AutoStartHosts(allowed)
}

func (s *ForwardService) ServiceShutdown() error {
	s.manager.Shutdown()
	return nil
}

func (s *ForwardService) hostExists(name string) bool {
	hosts, err := ParseSSHConfig(s.configPath)
	if err != nil {
		return false
	}
	name = strings.TrimSpace(name)
	for _, host := range hosts {
		if host.Name == name {
			return true
		}
	}
	return false
}
