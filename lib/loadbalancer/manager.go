/*
Copyright 2021 Gravitational, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package loadbalancer

import (
	"context"
	"strconv"
	"time"

	"github.com/gravitational/planet/lib/constants"
	"github.com/gravitational/planet/lib/loadbalancer/internal"
	fileutils "github.com/gravitational/planet/lib/utils/file"
	"github.com/gravitational/planet/lib/utils/systemd"

	"github.com/gravitational/trace"
	log "github.com/sirupsen/logrus"
)

// NewManager creates a new manager for loadbalancer
func NewManager() *Manager {
	return &Manager{}
}

// Storage stores current addresses for apiserver
type Storage interface {
	Put(ctx context.Context, key, value string)
	Remove(ctx context.Context, key string)
}

// Manager manages the haproxy setting and the start of the service
type Manager struct {
	storage *internal.Storage
}

// GetStorage return the storage object
// To get storage, the manager should be started
func (m *Manager) GetStorage() Storage {
	return m.storage
}

// Start is the main manager loop. It periodically reconciles the service based on configuration updates.
func (m *Manager) Start(ctx context.Context) {
	storageCh := make(chan string, 5)
	defer close(storageCh)
	m.storage = internal.NewStorage(storageCh)
	ticker := time.NewTicker(time.Second * 30)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := m.reconfigure(ctx); err != nil {
				log.Warn("unable to reconfigure HAProxy service", err)
			}
		case <-storageCh:
			if err := m.reconfigure(ctx); err != nil {
				log.Warn("unable to reconfigure HAProxy service", err)
			}
		case <-ctx.Done():
			return
		}
	}
}

// StopService disables and stops the service
func (m *Manager) StopService(ctx context.Context) error {
	err := systemd.DisableService(ctx, haproxyServiceName)
	if err != nil {
		return trace.Wrap(err)
	}
	return nil
}

func (m *Manager) ensureService(ctx context.Context, needRestart bool) error {
	status, err := systemd.GetServiceStatus(haproxyServiceName)
	if err != nil {
		return trace.Wrap(err, "unable to get service status %s", haproxyServiceName)
	}
	log.WithFields(log.Fields{
		"service": haproxyServiceName,
		"status":  status,
	}).Info("service info")
	if status == "active" && needRestart {
		err := systemd.Systemctl(ctx, "restart", haproxyServiceName)
		if err != nil {
			return trace.Wrap(err, "unable to restart service %s", haproxyServiceName)
		}
	}
	if status != "active" {
		err := systemd.EnableService(ctx, haproxyServiceName)
		if err != nil {
			return trace.Wrap(err, "unable to start service %s", haproxyServiceName)
		}
	}
	return nil
}

func (m *Manager) reconfigure(ctx context.Context) error {
	hosts := m.storage.GetHosts()
	if len(hosts) == 0 {
		return nil
	}

	kubeServers := make(map[string]string)
	for i, host := range hosts {
		key := "master-" + strconv.Itoa(i+1)
		kubeServers[key] = host + ":6443"
	}

	lbConfig, err := GenerateConfig(&ConfigData{
		KubePort:    9443,
		KubeServers: kubeServers,
	})
	if err != nil {
		return trace.Wrap(err)
	}
	configFile := &fileutils.File{
		Path: configPath,
		Data: lbConfig,
		Mode: constants.SharedReadMask,
	}

	changed, err := fileutils.EnsureFile(configFile)
	if err != nil {
		return trace.Wrap(err)
	}
	if err := m.ensureService(ctx, changed); err != nil {
		return trace.Wrap(err)
	}
	return nil
}
