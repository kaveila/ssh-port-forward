package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	ForwardLocal   = "local"
	ForwardRemote  = "remote"
	ForwardDynamic = "dynamic"
)

type ForwardSpec struct {
	ID          string `json:"id"`
	Host        string `json:"host"`
	Kind        string `json:"kind"`
	BindAddress string `json:"bindAddress"`
	LocalPort   int    `json:"localPort"`
	RemoteHost  string `json:"remoteHost,omitempty"`
	RemotePort  int    `json:"remotePort,omitempty"`
	AutoStart   bool   `json:"autoStart"`
}

type ForwardInput struct {
	Host        string `json:"host"`
	Kind        string `json:"kind"`
	BindAddress string `json:"bindAddress"`
	LocalPort   int    `json:"localPort"`
	RemoteHost  string `json:"remoteHost"`
	RemotePort  int    `json:"remotePort"`
	AutoStart   bool   `json:"autoStart"`
	StartNow    bool   `json:"startNow"`
}

type ForwardView struct {
	ForwardSpec
	Running   bool   `json:"running"`
	PID       int    `json:"pid"`
	StartedAt string `json:"startedAt,omitempty"`
	LastError string `json:"lastError,omitempty"`
}

type storedForwards struct {
	Version  int           `json:"version"`
	Forwards []ForwardSpec `json:"forwards"`
}

type managedForward struct {
	spec      ForwardSpec
	cmd       *exec.Cmd
	stderr    *cappedBuffer
	running   bool
	stopping  bool
	startedAt time.Time
	exitCh    chan struct{}
	lastError string
}

type ForwardManager struct {
	mu             sync.RWMutex
	storePath      string
	sshExecutable  string
	forwards       map[string]*managedForward
	order          []string
	loadError      string
	shuttingDown   bool
	commandFactory func(ForwardSpec) (*exec.Cmd, error)
}

func NewForwardManager(storePath, sshExecutable string) (*ForwardManager, error) {
	manager := &ForwardManager{
		storePath:     storePath,
		sshExecutable: sshExecutable,
		forwards:      make(map[string]*managedForward),
	}
	if err := manager.load(); err != nil {
		manager.loadError = err.Error()
	}
	return manager, nil
}

func (m *ForwardManager) load() error {
	data, err := os.ReadFile(m.storePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取转发配置失败: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil
	}

	var stored storedForwards
	if err := json.Unmarshal(data, &stored); err != nil {
		return fmt.Errorf("转发配置文件格式错误: %w", err)
	}
	for _, item := range stored.Forwards {
		normalized, err := normalizeForwardSpec(item)
		if err != nil || normalized.ID == "" {
			continue
		}
		if _, exists := m.forwards[normalized.ID]; exists {
			continue
		}
		m.forwards[normalized.ID] = &managedForward{spec: normalized}
		m.order = append(m.order, normalized.ID)
	}
	return nil
}

func (m *ForwardManager) StorageError() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.loadError
}

func (m *ForwardManager) Add(input ForwardInput) (ForwardView, error) {
	spec := ForwardSpec{
		Host:        input.Host,
		Kind:        input.Kind,
		BindAddress: input.BindAddress,
		LocalPort:   input.LocalPort,
		RemoteHost:  input.RemoteHost,
		RemotePort:  input.RemotePort,
		AutoStart:   input.AutoStart,
	}
	normalized, err := normalizeForwardSpec(spec)
	if err != nil {
		return ForwardView{}, err
	}
	normalized.ID = newForwardID()

	m.mu.Lock()
	m.forwards[normalized.ID] = &managedForward{spec: normalized}
	m.order = append(m.order, normalized.ID)
	if err := m.saveLocked(); err != nil {
		delete(m.forwards, normalized.ID)
		m.order = m.order[:len(m.order)-1]
		m.mu.Unlock()
		return ForwardView{}, err
	}
	view := m.viewLocked(m.forwards[normalized.ID])
	m.mu.Unlock()
	return view, nil
}

func (m *ForwardManager) Start(id string) (ForwardView, error) {
	m.mu.Lock()
	item, ok := m.forwards[id]
	if !ok {
		m.mu.Unlock()
		return ForwardView{}, errors.New("端口转发不存在")
	}
	if item.running {
		view := m.viewLocked(item)
		m.mu.Unlock()
		return view, nil
	}
	if m.shuttingDown {
		m.mu.Unlock()
		return ForwardView{}, errors.New("应用正在退出，无法启动端口转发")
	}

	var cmd *exec.Cmd
	var err error
	if m.commandFactory != nil {
		cmd, err = m.commandFactory(item.spec)
	} else {
		var sshPath string
		sshPath, err = exec.LookPath(m.sshExecutable)
		if err == nil {
			cmd = exec.Command(sshPath, buildSSHArgs(item.spec)...)
		}
	}
	if err != nil {
		item.lastError = fmt.Sprintf("启动 ssh 失败: %v", err)
		view := m.viewLocked(item)
		m.mu.Unlock()
		return view, errors.New(item.lastError)
	}
	if cmd == nil {
		item.lastError = "未找到 ssh 命令，请确认 OpenSSH Client 已安装并加入 PATH"
		view := m.viewLocked(item)
		m.mu.Unlock()
		return view, errors.New(item.lastError)
	}
	configureCommand(cmd)
	stderr := newCappedBuffer(12 * 1024)
	cmd.Stdout = io.Discard
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		item.lastError = fmt.Sprintf("启动 ssh 失败: %v", err)
		view := m.viewLocked(item)
		m.mu.Unlock()
		return view, errors.New(item.lastError)
	}

	exitCh := make(chan struct{})
	item.cmd = cmd
	item.stderr = stderr
	item.running = true
	item.stopping = false
	item.startedAt = time.Now()
	item.lastError = ""
	item.exitCh = exitCh
	m.mu.Unlock()

	go m.waitForProcess(item, cmd, stderr, exitCh)

	// ExitOnForwardFailure makes most setup failures happen immediately. This
	// short grace period lets the UI report those instead of flashing green.
	select {
	case <-exitCh:
		m.mu.RLock()
		defer m.mu.RUnlock()
		if item.lastError == "" {
			return m.viewLocked(item), errors.New("ssh 进程已退出")
		}
		return m.viewLocked(item), errors.New(item.lastError)
	case <-time.After(450 * time.Millisecond):
		m.mu.RLock()
		defer m.mu.RUnlock()
		return m.viewLocked(item), nil
	}
}

func (m *ForwardManager) waitForProcess(item *managedForward, cmd *exec.Cmd, stderr *cappedBuffer, exitCh chan struct{}) {
	err := cmd.Wait()

	m.mu.Lock()
	defer m.mu.Unlock()
	defer close(exitCh)

	if item.cmd != cmd {
		return
	}

	item.running = false
	item.cmd = nil
	if err != nil && !item.stopping {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		item.lastError = message
	}
	item.stopping = false
}

func (m *ForwardManager) Stop(id string) (ForwardView, error) {
	m.mu.RLock()
	item, ok := m.forwards[id]
	if !ok {
		m.mu.RUnlock()
		return ForwardView{}, errors.New("端口转发不存在")
	}
	if !item.running || item.cmd == nil || item.cmd.Process == nil {
		view := m.viewLocked(item)
		m.mu.RUnlock()
		return view, nil
	}
	process := item.cmd.Process
	exitCh := item.exitCh
	m.mu.RUnlock()

	m.mu.Lock()
	item.stopping = true
	m.mu.Unlock()

	if err := process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return ForwardView{}, fmt.Errorf("停止 ssh 失败: %w", err)
	}

	select {
	case <-exitCh:
	case <-time.After(4 * time.Second):
		return ForwardView{}, errors.New("等待 ssh 退出超时")
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	item.lastError = ""
	return m.viewLocked(item), nil
}

func (m *ForwardManager) Delete(id string) error {
	if _, err := m.Stop(id); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.forwards[id]; !ok {
		return errors.New("端口转发不存在")
	}
	removed := m.forwards[id]
	removedOrder := append([]string(nil), m.order...)
	delete(m.forwards, id)
	for i, current := range m.order {
		if current == id {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
	if err := m.saveLocked(); err != nil {
		m.forwards[id] = removed
		m.order = removedOrder
		return err
	}
	return nil
}

func (m *ForwardManager) Get(id string) (ForwardView, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	item, ok := m.forwards[id]
	if !ok {
		return ForwardView{}, errors.New("端口转发不存在")
	}
	return m.viewLocked(item), nil
}

func (m *ForwardManager) ListForHost(host string) []ForwardView {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]ForwardView, 0)
	for _, id := range m.order {
		item := m.forwards[id]
		if item != nil && item.spec.Host == host {
			result = append(result, m.viewLocked(item))
		}
	}
	return result
}

func (m *ForwardManager) CountsByHost() (configured, active map[string]int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	configured = make(map[string]int)
	active = make(map[string]int)
	for _, item := range m.forwards {
		configured[item.spec.Host]++
		if item.running {
			active[item.spec.Host]++
		}
	}
	return configured, active
}

func (m *ForwardManager) AutoStartHosts(allowed map[string]bool) {
	m.mu.RLock()
	var ids []string
	for _, id := range m.order {
		item := m.forwards[id]
		if item != nil && item.spec.AutoStart && allowed[item.spec.Host] {
			ids = append(ids, id)
		}
	}
	m.mu.RUnlock()

	for _, id := range ids {
		_, _ = m.Start(id)
	}
}

func (m *ForwardManager) Shutdown() {
	m.mu.Lock()
	if m.shuttingDown {
		m.mu.Unlock()
		return
	}
	m.shuttingDown = true
	var processes []*os.Process
	for _, item := range m.forwards {
		if item.running && item.cmd != nil && item.cmd.Process != nil {
			item.stopping = true
			processes = append(processes, item.cmd.Process)
		}
	}
	m.mu.Unlock()

	for _, process := range processes {
		_ = process.Kill()
	}
}

func (m *ForwardManager) saveLocked() error {
	payload := storedForwards{
		Version:  1,
		Forwards: make([]ForwardSpec, 0, len(m.order)),
	}
	for _, id := range m.order {
		if item := m.forwards[id]; item != nil {
			payload.Forwards = append(payload.Forwards, item.spec)
		}
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("编码转发配置失败: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(m.storePath), 0o700); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}

	temp, err := os.CreateTemp(filepath.Dir(m.storePath), "forwards-*.tmp")
	if err != nil {
		return fmt.Errorf("创建配置临时文件失败: %w", err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)

	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return fmt.Errorf("设置配置文件权限失败: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return fmt.Errorf("写入转发配置失败: %w", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return fmt.Errorf("同步转发配置失败: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("关闭转发配置失败: %w", err)
	}
	if err := os.Rename(tempName, m.storePath); err != nil {
		return fmt.Errorf("保存转发配置失败: %w", err)
	}
	return nil
}

func (m *ForwardManager) viewLocked(item *managedForward) ForwardView {
	view := ForwardView{
		ForwardSpec: item.spec,
		Running:     item.running,
		LastError:   item.lastError,
	}
	if item.running && item.cmd != nil && item.cmd.Process != nil {
		view.PID = item.cmd.Process.Pid
	}
	if !item.startedAt.IsZero() {
		view.StartedAt = item.startedAt.Format(time.RFC3339)
	}
	return view
}

func normalizeForwardSpec(spec ForwardSpec) (ForwardSpec, error) {
	spec.Host = strings.TrimSpace(spec.Host)
	spec.Kind = strings.ToLower(strings.TrimSpace(spec.Kind))
	spec.BindAddress = trimAddress(spec.BindAddress)
	spec.RemoteHost = trimAddress(spec.RemoteHost)

	if spec.Host == "" || strings.HasPrefix(spec.Host, "-") || containsUnsafeTokenRune(spec.Host) {
		return ForwardSpec{}, errors.New("Host 别名无效")
	}
	if spec.BindAddress == "" {
		spec.BindAddress = "127.0.0.1"
	}
	if containsUnsafeTokenRune(spec.BindAddress) {
		return ForwardSpec{}, errors.New("监听地址不能包含空白或控制字符")
	}
	if spec.LocalPort < 1 || spec.LocalPort > 65535 {
		return ForwardSpec{}, errors.New("监听端口必须在 1-65535 之间")
	}

	switch spec.Kind {
	case ForwardLocal, ForwardRemote:
		if spec.RemoteHost == "" || containsUnsafeTokenRune(spec.RemoteHost) {
			return ForwardSpec{}, errors.New("目标地址无效")
		}
		if spec.RemotePort < 1 || spec.RemotePort > 65535 {
			return ForwardSpec{}, errors.New("目标端口必须在 1-65535 之间")
		}
	case ForwardDynamic:
		spec.RemoteHost = ""
		spec.RemotePort = 0
	default:
		return ForwardSpec{}, errors.New("转发类型必须是 local、remote 或 dynamic")
	}

	return spec, nil
}

func buildSSHArgs(spec ForwardSpec) []string {
	args := []string{
		"-N",
		"-T",
		"-o", "BatchMode=yes",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "ConnectTimeout=15",
		"-o", "ServerAliveInterval=30",
		"-o", "ServerAliveCountMax=3",
		"-o", "LogLevel=ERROR",
	}

	listen := net.JoinHostPort(spec.BindAddress, strconv.Itoa(spec.LocalPort))
	switch spec.Kind {
	case ForwardLocal:
		target := net.JoinHostPort(spec.RemoteHost, strconv.Itoa(spec.RemotePort))
		args = append(args, "-L", listen+":"+target)
	case ForwardRemote:
		target := net.JoinHostPort(spec.RemoteHost, strconv.Itoa(spec.RemotePort))
		args = append(args, "-R", listen+":"+target)
	case ForwardDynamic:
		args = append(args, "-D", listen)
	}

	return append(args, "--", spec.Host)
}

func trimAddress(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && value[0] == '[' && value[len(value)-1] == ']' {
		return value[1 : len(value)-1]
	}
	return value
}

func containsUnsafeTokenRune(value string) bool {
	return strings.IndexFunc(value, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}) >= 0
}

func newForwardID() string {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err == nil {
		return hex.EncodeToString(random)
	}
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

type cappedBuffer struct {
	mu   sync.Mutex
	data []byte
	max  int
}

func newCappedBuffer(max int) *cappedBuffer {
	return &cappedBuffer{max: max}
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, p...)
	if len(b.data) > b.max {
		b.data = append([]byte(nil), b.data[len(b.data)-b.max:]...)
	}
	return len(p), nil
}

func (b *cappedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.data)
}
