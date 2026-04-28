package resource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	helmloader "helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chartutil"
	helmdriver "helm.sh/helm/v3/pkg/storage/driver"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"

	"github.com/sbkg0002/kubernetes-testing-framework/internal/decode"
	"github.com/sbkg0002/kubernetes-testing-framework/internal/helmutil"
)

// kindPriority controls apply order within a directory of manifests.
// Lower number = applied first.
var kindPriority = map[string]int{
	"Namespace":                0,
	"CustomResourceDefinition": 1,
	"ServiceAccount":           2,
	"ClusterRole":              3,
	"ClusterRoleBinding":       4,
	"Role":                     5,
	"RoleBinding":              6,
	"ConfigMap":                7,
	"Secret":                   8,
	"PersistentVolumeClaim":    9,
	"Service":                  10,
	"Deployment":               11,
	"StatefulSet":              11,
	"DaemonSet":                11,
	"Job":                      12,
	"CronJob":                  13,
}

func kindOrder(kind string) int {
	if p, ok := kindPriority[kind]; ok {
		return p
	}
	return 50
}

type manager struct {
	dynClient  dynamic.Interface
	mapper     *restmapper.DeferredDiscoveryRESTMapper
	helmConfig *action.Configuration
	namespace  string
	log        *slog.Logger
}

// NewManager constructs a Manager using the provided REST config.
func NewManager(restConfig *rest.Config, namespace string, log *slog.Logger) (Manager, error) {
	if namespace == "" {
		namespace = "default"
	}

	dynClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("creating dynamic client: %w", err)
	}

	discClient, err := discovery.NewDiscoveryClientForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("creating discovery client: %w", err)
	}
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(discClient))

	helmCfg, err := helmutil.NewActionConfig(restConfig, namespace, log)
	if err != nil {
		return nil, fmt.Errorf("creating helm config: %w", err)
	}

	return &manager{
		dynClient:  dynClient,
		mapper:     mapper,
		helmConfig: helmCfg,
		namespace:  namespace,
		log:        log,
	}, nil
}

// Apply deploys all resources.
func (m *manager) Apply(ctx context.Context, resources []Resource) error {
	m.mapper.Reset()
	for _, r := range resources {
		m.log.Info("applying resource", "name", r.Name, "kind", r.Kind)
		var err error
		switch r.Kind {
		case "manifest":
			err = m.applyManifest(ctx, r)
		case "helm":
			err = m.applyHelm(ctx, r)
		default:
			return fmt.Errorf("unknown resource kind %q", r.Kind)
		}
		if err != nil {
			return fmt.Errorf("applying %s %q: %w", r.Kind, r.Name, err)
		}
	}
	return nil
}

// loadObjects returns all Unstructured objects from a manifest Resource,
// fetching from a URL or reading from a local path as appropriate.
func (m *manager) loadObjects(ctx context.Context, r Resource) ([]*unstructured.Unstructured, error) {
	if r.ManifestURL != "" {
		return m.fetchURL(ctx, r.ManifestURL)
	}
	return m.loadPathObjects(r.ManifestPath)
}

func (m *manager) fetchURL(ctx context.Context, url string) ([]*unstructured.Unstructured, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building request for %s: %w", url, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s: HTTP %d", url, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", url, err)
	}
	objs, err := decode.Documents(data)
	if err != nil {
		return nil, fmt.Errorf("decoding %s: %w", url, err)
	}
	return objs, nil
}

func (m *manager) loadPathObjects(path string) ([]*unstructured.Unstructured, error) {
	var files []string
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".json") {
				files = append(files, filepath.Join(path, name))
			}
		}
	} else {
		files = []string{path}
	}

	var allObjs []*unstructured.Unstructured
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		objs, err := decode.Documents(data)
		if err != nil {
			return nil, fmt.Errorf("decoding %s: %w", f, err)
		}
		allObjs = append(allObjs, objs...)
	}
	return allObjs, nil
}

func (m *manager) applyManifest(ctx context.Context, r Resource) error {
	allObjs, err := m.loadObjects(ctx, r)
	if err != nil {
		return err
	}

	sort.SliceStable(allObjs, func(i, j int) bool {
		return kindOrder(allObjs[i].GetKind()) < kindOrder(allObjs[j].GetKind())
	})

	ns := r.Namespace
	if ns == "" {
		ns = m.namespace
	}
	for _, obj := range allObjs {
		if err := m.applyObject(ctx, obj, ns); err != nil {
			return fmt.Errorf("applying %s %q: %w", obj.GetKind(), obj.GetName(), err)
		}
	}
	return nil
}

func (m *manager) applyObject(ctx context.Context, obj *unstructured.Unstructured, defaultNS string) error {
	gvk := obj.GroupVersionKind()
	mapping, err := m.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return fmt.Errorf("mapping GVK %s: %w", gvk, err)
	}

	data, err := json.Marshal(obj)
	if err != nil {
		return fmt.Errorf("marshaling object: %w", err)
	}
	_ = data

	var resClient dynamic.ResourceInterface
	if mapping.Scope.Name() == meta.RESTScopeNameRoot {
		resClient = m.dynClient.Resource(mapping.Resource)
	} else {
		ns := obj.GetNamespace()
		if ns == "" {
			ns = defaultNS
		}
		obj.SetNamespace(ns)
		resClient = m.dynClient.Resource(mapping.Resource).Namespace(ns)
	}

	_, err = resClient.Apply(ctx, obj.GetName(), obj, metav1.ApplyOptions{
		FieldManager: "ktf",
		Force:        true,
	})
	return err
}

func (m *manager) applyHelm(ctx context.Context, r Resource) error {
	releaseName := r.Name
	ns := r.Namespace
	if ns == "" {
		ns = m.namespace
	}

	chart, err := helmloader.Load(r.HelmChart)
	if err != nil {
		return fmt.Errorf("loading chart %q: %w", r.HelmChart, err)
	}
	if releaseName == "" {
		releaseName = chart.Name()
	}

	var vals map[string]interface{}
	if r.HelmValues != "" {
		v, err := chartutil.ReadValuesFile(r.HelmValues)
		if err != nil {
			return fmt.Errorf("reading values %q: %w", r.HelmValues, err)
		}
		vals = v.AsMap()
	}

	// Check whether release already exists
	histAction := action.NewHistory(m.helmConfig)
	histAction.Max = 1
	_, histErr := histAction.Run(releaseName)

	if errors.Is(histErr, helmdriver.ErrReleaseNotFound) {
		installAction := action.NewInstall(m.helmConfig)
		installAction.Namespace = ns
		installAction.ReleaseName = releaseName
		installAction.CreateNamespace = true
		installAction.Wait = false
		_, err = installAction.RunWithContext(ctx, chart, vals)
		return err
	}
	if histErr != nil {
		return fmt.Errorf("fetching release history: %w", histErr)
	}

	upgradeAction := action.NewUpgrade(m.helmConfig)
	upgradeAction.Namespace = ns
	upgradeAction.Wait = false
	_, err = upgradeAction.RunWithContext(ctx, releaseName, chart, vals)
	return err
}

// WaitReady performs a single readiness check (no retry loop).
func (m *manager) WaitReady(ctx context.Context, resources []Resource) error {
	for _, r := range resources {
		var ready bool
		var err error
		switch r.Kind {
		case "manifest":
			ready, err = m.isManifestReady(ctx, r)
		case "helm":
			ready, err = m.isHelmReady(r)
		default:
			continue
		}
		if err != nil {
			return fmt.Errorf("checking readiness of %s %q: %w", r.Kind, r.Name, err)
		}
		if !ready {
			return ErrNotReady
		}
	}
	return nil
}

func (m *manager) isManifestReady(ctx context.Context, r Resource) (bool, error) {
	objs, err := m.loadObjects(ctx, r)
	if err != nil {
		return false, nil // transient: path missing or URL unreachable, keep polling
	}
	for _, obj := range objs {
		ready, err := m.isObjectReady(ctx, obj, r.Namespace)
		if err != nil || !ready {
			return false, nil
		}
	}
	return true, nil
}

func (m *manager) isObjectReady(ctx context.Context, obj *unstructured.Unstructured, defaultNS string) (bool, error) {
	kind := obj.GetKind()
	switch kind {
	case "Deployment", "StatefulSet", "DaemonSet":
		return m.isWorkloadReady(ctx, obj, defaultNS)
	case "Job":
		return m.isJobComplete(ctx, obj, defaultNS)
	default:
		// ConfigMap, Secret, Service, etc. are immediately considered ready
		return true, nil
	}
}

func (m *manager) isWorkloadReady(ctx context.Context, obj *unstructured.Unstructured, defaultNS string) (bool, error) {
	gvk := obj.GroupVersionKind()
	mapping, err := m.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return false, nil
	}

	ns := obj.GetNamespace()
	if ns == "" {
		ns = defaultNS
	}
	if ns == "" {
		ns = m.namespace
	}

	live, err := m.dynClient.Resource(mapping.Resource).Namespace(ns).Get(ctx, obj.GetName(), metav1.GetOptions{})
	if err != nil {
		return false, nil // not found yet
	}

	desired, _, _ := unstructured.NestedInt64(live.Object, "spec", "replicas")
	if desired == 0 {
		desired = 1
	}
	ready, _, _ := unstructured.NestedInt64(live.Object, "status", "readyReplicas")
	return ready >= desired, nil
}

func (m *manager) isJobComplete(ctx context.Context, obj *unstructured.Unstructured, defaultNS string) (bool, error) {
	gvk := schema.GroupVersionKind{Group: "batch", Version: "v1", Kind: "Job"}
	mapping, err := m.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return false, nil
	}

	ns := obj.GetNamespace()
	if ns == "" {
		ns = defaultNS
	}
	if ns == "" {
		ns = m.namespace
	}

	live, err := m.dynClient.Resource(mapping.Resource).Namespace(ns).Get(ctx, obj.GetName(), metav1.GetOptions{})
	if err != nil {
		return false, nil
	}

	conditions, _, _ := unstructured.NestedSlice(live.Object, "status", "conditions")
	for _, cond := range conditions {
		c, ok := cond.(map[string]interface{})
		if !ok {
			continue
		}
		if c["type"] == "Complete" && c["status"] == "True" {
			return true, nil
		}
	}
	return false, nil
}

func (m *manager) isHelmReady(r Resource) (bool, error) {
	releaseName := r.Name
	statusAction := action.NewStatus(m.helmConfig)
	rel, err := statusAction.Run(releaseName)
	if err != nil {
		return false, nil // release may not exist yet
	}
	return rel.Info != nil && rel.Info.Status.IsPending() == false, nil
}

// Delete removes all resources in reverse order.
func (m *manager) Delete(ctx context.Context, resources []Resource) error {
	// Reverse
	reversed := make([]Resource, len(resources))
	copy(reversed, resources)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}

	for _, r := range reversed {
		m.log.Info("deleting resource", "name", r.Name, "kind", r.Kind)
		var err error
		switch r.Kind {
		case "manifest":
			err = m.deleteManifest(ctx, r)
		case "helm":
			err = m.deleteHelm(r)
		}
		if err != nil {
			m.log.Error("error during teardown", "name", r.Name, "error", err)
			// continue with remaining resources
		}
	}
	return nil
}

func (m *manager) deleteManifest(ctx context.Context, r Resource) error {
	allObjs, err := m.loadObjects(ctx, r)
	if err != nil {
		return nil // already gone or unreachable; skip silently
	}

	// Delete in reverse kind priority
	sort.SliceStable(allObjs, func(i, j int) bool {
		return kindOrder(allObjs[i].GetKind()) > kindOrder(allObjs[j].GetKind())
	})

	ns := r.Namespace
	if ns == "" {
		ns = m.namespace
	}
	for _, obj := range allObjs {
		gvk := obj.GroupVersionKind()
		mapping, err := m.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			continue
		}
		var resClient dynamic.ResourceInterface
		if mapping.Scope.Name() == meta.RESTScopeNameRoot {
			resClient = m.dynClient.Resource(mapping.Resource)
		} else {
			objNS := obj.GetNamespace()
			if objNS == "" {
				objNS = ns
			}
			resClient = m.dynClient.Resource(mapping.Resource).Namespace(objNS)
		}
		propagation := metav1.DeletePropagationForeground
		_ = resClient.Delete(ctx, obj.GetName(), metav1.DeleteOptions{PropagationPolicy: &propagation})
	}
	return nil
}

func (m *manager) deleteHelm(r Resource) error {
	uninstall := action.NewUninstall(m.helmConfig)
	uninstall.Wait = false
	_, err := uninstall.Run(r.Name)
	if errors.Is(err, helmdriver.ErrReleaseNotFound) {
		return nil
	}
	return err
}
