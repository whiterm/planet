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
	"bytes"
	"text/template"

	"github.com/gravitational/trace"
)

// ConfigData stores the data for the loadbalancer configuration template
type ConfigData struct {
	KubePort    int
	KubeServers map[string]string
}

// configTemplate is the loadbalancer config template
var configTemplate = template.Must(template.New("loadbalancer-config").Parse(`# generated by planet
global
  log /dev/log local0
  log /dev/log local1 notice
  daemon
defaults
  log global
  maxconn 20000
  mode tcp
  option dontlognull
  timeout http-request 10s
  timeout queue        1m
  timeout connect      10s
  timeout client       86400s
  timeout server       86400s
  timeout tunnel       86400s
frontend control-plane
  bind *:{{ .KubePort }}
  default_backend kube-apiservers
backend kube-apiservers
  option httpchk GET /healthz
  option log-health-checks
  http-check expect status 200
  mode tcp
  balance roundrobin
  default-server verify none inter 5s downinter 5s rise 2 fall 2 slowstart 60s maxconn 5000 maxqueue 5000 weight 100
  {{range $server, $address := .KubeServers}}
  server {{ $server }} {{ $address }} check check-ssl crt /etc/haproxy/certs/haproxy.pem
  {{- end}}
`))

// GenerateConfig returns a configuration for haproxy generated from the config data
func GenerateConfig(data *ConfigData) ([]byte, error) {
	var buff bytes.Buffer
	err := configTemplate.Execute(&buff, data)
	if err != nil {
		return nil, trace.Wrap(err, "error executing haproxy config template")
	}
	return buff.Bytes(), nil
}