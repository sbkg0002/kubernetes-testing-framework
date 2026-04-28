package resource

import (
	"fmt"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// BuildRestConfig returns a *rest.Config using the following priority:
//  1. Explicit kubeconfigPath (if non-empty)
//  2. In-cluster service account credentials
//  3. Default kubeconfig ($KUBECONFIG or ~/.kube/config)
func BuildRestConfig(kubeconfigPath string) (*rest.Config, error) {
	if kubeconfigPath != "" {
		cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		if err != nil {
			return nil, fmt.Errorf("loading kubeconfig %q: %w", kubeconfigPath, err)
		}
		return cfg, nil
	}

	// Try in-cluster first
	if cfg, err := rest.InClusterConfig(); err == nil {
		return cfg, nil
	}

	// Fall back to default kubeconfig (respects $KUBECONFIG env var)
	cfg, err := clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
	if err != nil {
		return nil, fmt.Errorf("loading default kubeconfig: %w", err)
	}
	return cfg, nil
}
