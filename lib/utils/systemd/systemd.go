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

package systemd

import (
	"context"
	"os/exec"

	"github.com/coreos/go-systemd/v22/dbus"
	"github.com/gravitational/trace"
	log "github.com/sirupsen/logrus"
)

// Systemctl runs a local systemctl command in non-blocking mode.
// TODO(knisbet): I'm using systemctl here, because using go-systemd and dbus appears to be unreliable, with
// masking unit files not working. Ideally, this will use dbus at some point in the future.
func Systemctl(ctx context.Context, operation, service string) error {
	return SystemctlCmd(ctx, operation, service, "--no-block")
}

// SystemctlCmd executes the command for systemctl
func SystemctlCmd(ctx context.Context, operation, service string, args ...string) error {
	args = append([]string{operation, service}, args...)
	out, err := exec.CommandContext(ctx, "/bin/systemctl", args...).CombinedOutput()
	log.WithFields(log.Fields{
		"operation": operation,
		"output":    string(out),
		"service":   service,
	}).Info("Execute systemctl.")
	if err != nil {
		return trace.Wrap(err, "failed to execute systemctl: %s", out).AddFields(map[string]interface{}{
			"operation": operation,
			"service":   service,
		})
	}
	return nil
}

// TryRestartService will request for systemd to restart a system service
func TryRestartService(ctx context.Context, service string) {
	// ignoring error results is intentional
	err := Systemctl(ctx, "restart", service)
	if err != nil {
		log.Debug("error attempting to restart service", err)
	}
}

// DisableService disables the service
func DisableService(ctx context.Context, service string) error {
	err := Systemctl(ctx, "mask", service)
	if err != nil {
		return trace.Wrap(err)
	}
	err = Systemctl(ctx, "stop", service)
	if err != nil {
		return trace.Wrap(err)
	}
	return nil
}

// EnableService enables the service
func EnableService(ctx context.Context, service string) error {
	err := Systemctl(ctx, "unmask", service)
	if err != nil {
		return trace.Wrap(err)
	}
	return trace.Wrap(Systemctl(ctx, "start", service))
}

// GetServiceStatus returns the service status
func GetServiceStatus(service string) (string, error) {
	conn, err := dbus.New()
	if err != nil {
		return "", trace.Wrap(err)
	}
	defer conn.Close()

	status, err := conn.ListUnitsByNames([]string{service})
	if err != nil {
		return "", trace.Wrap(err)
	}
	if len(status) != 1 {
		return "", trace.BadParameter("unexpected number of status results when checking service '%q'", service)
	}

	return status[0].ActiveState, nil
}