package loadbalancer

import (
	"context"
	"strconv"
	"strings"
	"time"

	fileutils "github.com/gravitational/planet/lib/utils/file"
	"github.com/gravitational/planet/lib/utils/systemd"
	"github.com/gravitational/trace"

	log "github.com/sirupsen/logrus"
)

func NewManager() *Manager {
	storageCh := make(chan string, 5)
	return &Manager{
		storage:   NewStorage(storageCh),
		storageCh: storageCh,
	}
}

type Manager struct {
	storage   *Storage
	storageCh <-chan string
}

func (m *Manager) GetStorage() *Storage {
	return m.storage
}

func (m *Manager) Run() {
	ticker := time.NewTicker(time.Second * 30)
	for {
		select {
		case <-ticker.C:
			if err := m.reconfigure(); err != nil {
				log.Warn("unable to reconfigure HAProxy service", err)
			}
		case <-m.storageCh:
			if err := m.reconfigure(); err != nil {
				log.Warn("unable to reconfigure HAProxy service", err)
			}
		}
	}
}

// StopService disables and stops the service
func (m *Manager) StopService() error {
	err := systemd.DisableService(context.Background(), "haproxy.service")
	if err != nil {
		return trace.Wrap(err)
	}
	return nil
}

func (m *Manager) ensureService(needRestart bool) error {
	service := "haproxy.service"
	status, err := systemd.GetServiceStatus(service)
	if err != nil {
		return trace.Wrap(err, "Unable to get service status %s", service)
	}
	log.WithFields(log.Fields{
		"service": service,
		"status":  status,
	}).Info("service info")
	if status == "active" && needRestart {
		err := systemd.Systemctl(context.Background(), "restart", service)
		if err != nil {
			return trace.Wrap(err, "Unable to restart service %s", service)
		}
	}
	if status != "active" {
		err := systemd.Systemctl(context.Background(), "start", service)
		if err != nil {
			return trace.Wrap(err, "Unable to start service %s", service)
		}
	}
	return nil
}

func (m *Manager) reconfigure() error {
	hosts := m.storage.GetHosts()
	if len(hosts) == 0 {
		return nil
	}

	kubeServers := make(map[string]string)
	registryServers := make(map[string]string)
	for i, host := range hosts {
		key := "master-" + strconv.Itoa(i+1)
		kubeServers[key] = host
		registryServers[key] = strings.Split(host, ":")[0] + ":5001"

	}

	lbConfig, err := LBConfig(&ConfigData{
		KubePort:        9443,
		KubeServers:     kubeServers,
		RegistryPort:    5000,
		RegistryServers: registryServers,
	})
	if err != nil {
		return trace.Wrap(err)
	}
	configFile := &fileutils.File{
		Path: ConfigPath,
		Data: lbConfig,
		Mode: 0644,
	}

	changed, err := fileutils.EnsureFile(configFile)
	if err != nil {
		return trace.Wrap(err)
	}
	if err := m.ensureService(changed); err != nil {
		return trace.Wrap(err)
	}
	return nil
}
