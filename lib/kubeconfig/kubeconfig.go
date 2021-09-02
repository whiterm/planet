package kubeconfig

import (
	"bytes"

	"github.com/gravitational/planet/lib/box"
	"github.com/gravitational/trace"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
)

// Kubeconfig defines kubernetes Kubeconfig file
type Kubeconfig struct {
	filepath  string
	apiConfig *api.Config
}

// Bytes serializes the config to yaml
func (k *Kubeconfig) Bytes() ([]byte, error) {
	content, err := clientcmd.Write(*k.apiConfig)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	return content, nil
}

// BuildFile creates box.File with kubeconfig
func (k *Kubeconfig) BuildFile(owners *box.FileOwner) (*box.File, error) {
	content, err := k.Bytes()
	if err != nil {
		return nil, trace.Wrap(err)
	}
	return &box.File{
		Path:     k.filepath,
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

// GenerateSimpleKubeConfig creates Kubeconfig
func GenerateSimpleKubeConfig(o Options) *Kubeconfig {
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
	return &Kubeconfig{
		filepath:  o.Filepath,
		apiConfig: &apiConfig,
	}
}
