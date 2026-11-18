package factoryreset

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/dchauviere/speakerctld/internal/systemd"
)

const markerName = ".speakerctld-factory-reset"

// Manager records reset requests before any data is erased.
type Manager struct {
	dir     string
	bootID  string
	mu      sync.Mutex
	pending bool
	request chan struct{}
	reboot  func() error
}

func New(dataDir string) (*Manager, error) {
	dir, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, err
	}
	dir = filepath.Clean(dir)
	if dir == "/" {
		return nil, errors.New("factory reset data directory cannot be the filesystem root")
	}
	id, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return nil, fmt.Errorf("read boot ID: %w", err)
	}
	return &Manager{dir: dir, bootID: strings.TrimSpace(string(id)), request: make(chan struct{}, 1), reboot: requestReboot}, nil
}

// Requests is signalled once a durable reset request has been written.
func (m *Manager) Requests() <-chan struct{} { return m.request }

func (m *Manager) Request() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pending {
		return errors.New("factory reset already requested")
	}
	if err := os.MkdirAll(m.dir, 0755); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	if err := m.checkDirectory(); err != nil {
		return err
	}
	f, err := os.CreateTemp(m.dir, ".speakerctld-reset-")
	if err != nil {
		return fmt.Errorf("create reset marker temp file: %w", err)
	}
	defer os.Remove(f.Name())
	_, writeErr := f.WriteString(m.bootID + "\n")
	if writeErr == nil {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		return fmt.Errorf("write reset marker: %w", errors.Join(writeErr, closeErr))
	}
	if err := os.Link(f.Name(), filepath.Join(m.dir, markerName)); err != nil {
		return fmt.Errorf("install reset marker: %w", err)
	}
	m.pending = true
	defer func() { m.request <- struct{}{} }()
	if err := syncDirectory(m.dir); err != nil {
		return fmt.Errorf("sync reset marker: %w", err)
	}
	log.Print("Factory reset requested")
	return nil
}

// Recover completes an interrupted reset before any other component starts.
// A reset requested during this boot must reboot before the daemon resumes.
func (m *Manager) Recover() error {
	data, err := os.ReadFile(filepath.Join(m.dir, markerName))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read reset marker: %w", err)
	}
	id := strings.TrimSpace(string(data))
	if id == "" {
		return errors.New("invalid factory reset marker")
	}
	log.Print("Resuming factory reset")
	if err := m.clear(); err != nil {
		return err
	}
	if id == m.bootID {
		return m.reboot()
	}
	if err := os.Remove(filepath.Join(m.dir, markerName)); err != nil {
		return fmt.Errorf("remove reset marker: %w", err)
	}
	return syncDirectory(m.dir)
}

// Execute erases the data directory and asks logind to reboot.
// The marker remains until the next boot to make interruption recoverable.
func (m *Manager) Execute() error {
	if err := m.clear(); err != nil {
		return err
	}
	return m.reboot()
}

func (m *Manager) checkDirectory() error {
	info, err := os.Lstat(m.dir)
	if err != nil {
		return fmt.Errorf("inspect data directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("data directory must be a real directory")
	}
	resolved, err := filepath.EvalSymlinks(m.dir)
	if err != nil {
		return fmt.Errorf("resolve data directory: %w", err)
	}
	if resolved != m.dir {
		return errors.New("data directory must not traverse symlinks")
	}
	// Never traverse another mounted filesystem, including bind mounts.
	mounts, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return fmt.Errorf("read mount information: %w", err)
	}
	for _, line := range strings.Split(string(mounts), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		path := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`).Replace(fields[4])
		if strings.HasPrefix(path, m.dir+string(filepath.Separator)) {
			return fmt.Errorf("nested mount in data directory: %s", path)
		}
	}
	return nil
}

func (m *Manager) clear() error {
	if err := m.checkDirectory(); err != nil {
		return err
	}
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return fmt.Errorf("read data directory: %w", err)
	}
	for _, entry := range entries {
		if entry.Name() == markerName {
			continue
		}
		if err := os.RemoveAll(filepath.Join(m.dir, entry.Name())); err != nil {
			return fmt.Errorf("remove %s: %w", entry.Name(), err)
		}
	}
	if err := syncDirectory(m.dir); err != nil {
		return fmt.Errorf("sync data directory: %w", err)
	}
	log.Print("Factory reset data erased")
	return nil
}

func syncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func requestReboot() error {
	if err := systemd.Reboot(); err != nil {
		return err
	}
	// Do not start the daemon again while the reboot is pending.
	return errors.New("factory reset reboot requested")
}
