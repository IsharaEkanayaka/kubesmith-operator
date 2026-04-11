// Package helm provides in-cluster Helm operations for the kubesmith operator.
package helm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/storage/driver"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/yaml"

	kubesmithv1alpha1 "github.com/kubesmith/kubesmith-operator/api/v1alpha1"
)

// Client runs Helm operations against the cluster using the operator's
// in-cluster REST config — no SSH or external kubeconfig required.
type Client struct {
	restConfig *rest.Config
}

// NewClient creates a Helm Client backed by the given REST config.
func NewClient(cfg *rest.Config) *Client {
	return &Client{restConfig: cfg}
}

// actionConfig builds a Helm action.Configuration for the given namespace.
func (c *Client) actionConfig(ns string) (*action.Configuration, error) {
	getter := &restClientGetter{restConfig: c.restConfig, namespace: ns}
	cfg := new(action.Configuration)
	// Silent logger — controller's own logger handles Helm events.
	if err := cfg.Init(getter, ns, "secret", func(string, ...interface{}) {}); err != nil {
		return nil, fmt.Errorf("helm action config for ns %q: %w", ns, err)
	}
	return cfg, nil
}

// InstallOrUpgrade installs the Helm chart described by spec, or upgrades the
// existing release if one already exists. defaultReleaseName is used when
// spec.ReleaseName is empty.
func (c *Client) InstallOrUpgrade(
	ctx context.Context,
	ns string,
	spec *kubesmithv1alpha1.HelmSpec,
	defaultReleaseName string,
) error {
	releaseName := defaultReleaseName
	if spec.ReleaseName != "" {
		releaseName = spec.ReleaseName
	}

	actionCfg, err := c.actionConfig(ns)
	if err != nil {
		return err
	}

	// Download the chart archive to a temp directory.
	tmpDir, err := os.MkdirTemp("", "kubesmith-helm-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	settings := cli.New()
	settings.SetNamespace(ns)

	pullCfg, err := c.actionConfig(ns)
	if err != nil {
		return err
	}

	pull := action.NewPullWithOpts(action.WithConfig(pullCfg))
	pull.Settings = settings
	pull.RepoURL = spec.RepoURL
	pull.Version = spec.Version
	pull.Untar = true
	pull.UntarDir = tmpDir

	if _, err := pull.Run(spec.Chart); err != nil {
		return fmt.Errorf("helm pull %q from %q: %w", spec.Chart, spec.RepoURL, err)
	}

	chart, err := loader.Load(filepath.Join(tmpDir, spec.Chart))
	if err != nil {
		return fmt.Errorf("load chart %q: %w", spec.Chart, err)
	}

	// Parse caller-supplied values YAML.
	vals := map[string]interface{}{}
	if spec.Values != "" {
		if err := yaml.Unmarshal([]byte(spec.Values), &vals); err != nil {
			return fmt.Errorf("parse helm values: %w", err)
		}
	}

	// Decide install vs. upgrade.
	hist := action.NewHistory(actionCfg)
	hist.Max = 1
	_, histErr := hist.Run(releaseName)

	switch histErr {
	case driver.ErrReleaseNotFound:
		install := action.NewInstall(actionCfg)
		install.ReleaseName = releaseName
		install.Namespace = ns
		install.CreateNamespace = true
		install.Wait = true
		install.Timeout = 5 * time.Minute
		if _, err := install.RunWithContext(ctx, chart, vals); err != nil {
			return fmt.Errorf("helm install %q: %w", releaseName, err)
		}

	case nil:
		upgrade := action.NewUpgrade(actionCfg)
		upgrade.Namespace = ns
		upgrade.Wait = true
		upgrade.Timeout = 5 * time.Minute
		upgrade.ReuseValues = true
		if _, err := upgrade.RunWithContext(ctx, releaseName, chart, vals); err != nil {
			return fmt.Errorf("helm upgrade %q: %w", releaseName, err)
		}

	default:
		return fmt.Errorf("check release history for %q: %w", releaseName, histErr)
	}

	return nil
}

// Uninstall removes a Helm release. Succeeds silently if the release does not exist.
func (c *Client) Uninstall(ctx context.Context, ns, releaseName string) error {
	actionCfg, err := c.actionConfig(ns)
	if err != nil {
		return err
	}

	hist := action.NewHistory(actionCfg)
	hist.Max = 1
	if _, err := hist.Run(releaseName); err == driver.ErrReleaseNotFound {
		return nil // already gone
	} else if err != nil {
		return fmt.Errorf("check release history for %q: %w", releaseName, err)
	}

	if _, err := action.NewUninstall(actionCfg).Run(releaseName); err != nil {
		return fmt.Errorf("helm uninstall %q: %w", releaseName, err)
	}
	return nil
}
