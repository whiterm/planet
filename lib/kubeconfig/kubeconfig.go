package kubeconfig

import (
	"bytes"

	"github.com/gravitational/planet/lib/box"
	"github.com/gravitational/trace"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
)

// Config defines kubernetes Kubeconfig file
type Config struct {
	filepath  string
	apiConfig *api.Config
}

// Bytes serializes the config to yaml
func (c *Config) Bytes() ([]byte, error) {
	content, err := clientcmd.Write(*c.apiConfig)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	return content, nil
}

// BuildFile creates box.File with kubeconfig
func (c *Config) BuildFile(owners *box.FileOwner) (*box.File, error) {
	content, err := c.Bytes()
	if err != nil {
		return nil, trace.Wrap(err)
	}
	return &box.File{
		Path:     c.filepath,
		Contents: bytes.NewReader(content),
		Mode:     0644,
		Owners:   owners,
	}, nil
}

// Options is the parameters to generate kubeconfig
type Options struct {
	// Filepath is the path of the kubeconfig file
	Filepath string
	// Username is the name of the authInfo for the default context
	Username string
	// Server is the address of the kubernetes cluster (https://hostname:port).
	Server string
	// CertificateAuthority is the path to a cert file for the certificate authority.
	CertificateAuthority string
	// ClientKey is the path to a client key file for TLS.
	ClientKey string
	// ClientCertificate is the path to a client cert file for TLS.
	ClientCertificate string
}

// GenerateSimpleConfig creates Config
func GenerateSimpleConfig(o Options) *Config {
	clusterName := "kubernetes"
	contextName := "default"
	clusters := make(map[string]*api.Cluster)
	clusters[clusterName] = &api.Cluster{
		Server:               o.Server,
		CertificateAuthority: o.CertificateAuthority,
	}

	contexts := make(map[string]*api.Context)
	contexts[contextName] = &api.Context{
		Cluster:  clusterName,
		AuthInfo: o.Username,
	}

	authinfos := make(map[string]*api.AuthInfo)
	authinfos[o.Username] = &api.AuthInfo{
		ClientCertificate: o.ClientCertificate,
		ClientKey:         o.ClientKey,
	}

	apiConfig := api.Config{
		Clusters:       clusters,
		Contexts:       contexts,
		CurrentContext: contextName,
		AuthInfos:      authinfos,
	}
	return &Config{
		filepath:  o.Filepath,
		apiConfig: &apiConfig,
	}
}
