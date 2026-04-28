package helmutil

import (
	"fmt"
	"log/slog"

	"helm.sh/helm/v3/pkg/action"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

// NewActionConfig builds a Helm action.Configuration from an existing *rest.Config.
// namespace is where Helm will store release secrets.
func NewActionConfig(restConfig *rest.Config, namespace string, log *slog.Logger) (*action.Configuration, error) {
	if namespace == "" {
		namespace = "default"
	}

	getter := &restClientGetter{restConfig: restConfig, namespace: namespace}

	actionConfig := new(action.Configuration)
	debugLog := func(format string, v ...interface{}) {
		log.Debug(fmt.Sprintf(format, v...))
	}
	if err := actionConfig.Init(getter, namespace, "secrets", debugLog); err != nil {
		return nil, fmt.Errorf("initialising helm action config: %w", err)
	}
	return actionConfig, nil
}

// restClientGetter implements genericclioptions.RESTClientGetter over a *rest.Config.
// This lets Helm use an already-constructed config instead of reading from disk.
type restClientGetter struct {
	restConfig *rest.Config
	namespace  string
}

var _ genericclioptions.RESTClientGetter = &restClientGetter{}

func (r *restClientGetter) ToRESTConfig() (*rest.Config, error) {
	return r.restConfig, nil
}

func (r *restClientGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	dc, err := discovery.NewDiscoveryClientForConfig(r.restConfig)
	if err != nil {
		return nil, err
	}
	return memory.NewMemCacheClient(dc), nil
}

func (r *restClientGetter) ToRESTMapper() (meta.RESTMapper, error) {
	dc, err := r.ToDiscoveryClient()
	if err != nil {
		return nil, err
	}
	return restmapper.NewDeferredDiscoveryRESTMapper(dc), nil
}

func (r *restClientGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig {
	return &namespaceClientConfig{namespace: r.namespace}
}

// namespaceClientConfig is a minimal clientcmd.ClientConfig that returns a
// fixed namespace. Helm uses it only to resolve the target namespace.
type namespaceClientConfig struct {
	namespace string
}

func (n *namespaceClientConfig) RawConfig() (clientcmdapi.Config, error) {
	return *clientcmdapi.NewConfig(), nil
}

func (n *namespaceClientConfig) ClientConfig() (*rest.Config, error) {
	return nil, fmt.Errorf("namespaceClientConfig.ClientConfig not implemented")
}

func (n *namespaceClientConfig) Namespace() (string, bool, error) {
	if n.namespace == "" {
		return "default", false, nil
	}
	return n.namespace, true, nil
}

func (n *namespaceClientConfig) ConfigAccess() clientcmd.ConfigAccess {
	return nil
}
