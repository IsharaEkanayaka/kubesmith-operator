package helm

import (
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// restClientGetter implements genericclioptions.RESTClientGetter using an
// existing *rest.Config, enabling in-cluster Helm action config initialisation.
type restClientGetter struct {
	restConfig *rest.Config
	namespace  string
}

func (r *restClientGetter) ToRESTConfig() (*rest.Config, error) {
	return rest.CopyConfig(r.restConfig), nil
}

func (r *restClientGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	cfg, err := r.ToRESTConfig()
	if err != nil {
		return nil, err
	}
	disc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return memory.NewMemCacheClient(disc), nil
}

func (r *restClientGetter) ToRESTMapper() (meta.RESTMapper, error) {
	disc, err := r.ToDiscoveryClient()
	if err != nil {
		return nil, err
	}
	return restmapper.NewDeferredDiscoveryRESTMapper(disc), nil
}

// ToRawKubeConfigLoader returns a ClientConfig whose Namespace() method
// reports r.namespace. Helm uses this primarily for namespace resolution;
// the actual REST calls go through ToRESTConfig().
func (r *restClientGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig {
	cfg := clientcmdapi.NewConfig()
	cfg.Clusters["in-cluster"] = &clientcmdapi.Cluster{}
	cfg.AuthInfos["in-cluster"] = &clientcmdapi.AuthInfo{}
	cfg.Contexts["in-cluster"] = &clientcmdapi.Context{
		Cluster:   "in-cluster",
		AuthInfo:  "in-cluster",
		Namespace: r.namespace,
	}
	cfg.CurrentContext = "in-cluster"

	return clientcmd.NewDefaultClientConfig(
		*cfg,
		&clientcmd.ConfigOverrides{
			Context: clientcmdapi.Context{Namespace: r.namespace},
		},
	)
}
