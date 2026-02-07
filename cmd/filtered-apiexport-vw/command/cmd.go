/*
Copyright 2021 The KCP Authors.

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

package command

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	kcpclientset "github.com/kcp-dev/sdk/client/clientset/versioned/cluster"
	kcpinformers "github.com/kcp-dev/sdk/client/informers/externalversions"
	"github.com/spf13/cobra"

	"github.com/kcp-dev/contrib-filteredapiexport-vw/cmd/filtered-apiexport-vw/options"
	"github.com/kcp-dev/contrib-filteredapiexport-vw/internal/reconciler"
	filteredapiexportclientset "github.com/kcp-dev/contrib-filteredapiexport-vw/sdk/client/clientset/versioned/cluster"
	filteredapiexportinformers "github.com/kcp-dev/contrib-filteredapiexport-vw/sdk/client/informers/externalversions"

	kcpfeatures "github.com/kcp-dev/kcp/pkg/features"
	"github.com/kcp-dev/kcp/pkg/server/bootstrap"
	virtualrootapiserver "github.com/kcp-dev/kcp/pkg/virtual/framework/rootapiserver"
	virtualoptions "github.com/kcp-dev/kcp/pkg/virtual/options"

	kcpkubernetesclient "github.com/kcp-dev/client-go/kubernetes"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/wait"
	genericapiserver "k8s.io/apiserver/pkg/server"
	"k8s.io/client-go/pkg/version"
	"k8s.io/client-go/tools/clientcmd"
	logsapiv1 "k8s.io/component-base/logs/api/v1"
	"k8s.io/klog/v2"
)

func NewCommand(ctx context.Context, errout io.Writer) *cobra.Command {
	opts := options.NewOptions()

	// Default to -v=2
	opts.Logs.Verbosity = logsapiv1.VerbosityLevel(2)

	cmd := &cobra.Command{
		Use:   "workspaces",
		Short: "Launch virtual workspace apiservers",
		Long:  "Start the root virtual workspace apiserver to enable virtual workspace management.",

		RunE: func(c *cobra.Command, args []string) error {
			if err := logsapiv1.ValidateAndApply(opts.Logs, kcpfeatures.DefaultFeatureGate); err != nil {
				return err
			}
			if err := opts.Validate(); err != nil {
				return err
			}
			return Run(ctx, opts)
		},
	}

	opts.AddFlags(cmd.Flags())

	return cmd
}

// Run takes the options, starts the API server and waits until stopCh is closed or initial listening fails.
func Run(ctx context.Context, o *options.Options) error {
	logger := klog.FromContext(ctx).WithValues("component", "virtual-workspaces")
	// parse local shard kubeconfig
	localShardKubeconfig, err := readKubeConfig(o.LocalShardKubeconfigFile, o.Context)
	if err != nil {
		return err
	}
	localShardNonIdentityConfig, err := localShardKubeconfig.ClientConfig()
	if err != nil {
		return err
	}

	// parse front proxy kubeconfig
	frontProxyKubeconfig, err := readKubeConfig(o.FrontProxyKubeconfigFile, o.Context)
	if err != nil {
		return err
	}
	frontProxyNonIdentityConfig, err := frontProxyKubeconfig.ClientConfig()
	if err != nil {
		return err
	}

	// parse FilteredAPIExportEndpointSlices Virtual Workspace kubeconfig
	filteredAPIExportKubeconfig, err := readKubeConfig(o.FilteredAPIExportVirtualWorkspace.KubeconfigFile, o.Context)
	if err != nil {
		return err
	}
	filteredAPIExportConfig, err := filteredAPIExportKubeconfig.ClientConfig()
	if err != nil {
		return err
	}
	filteredAPIExportClusterClient, err := filteredapiexportclientset.NewForConfig(filteredAPIExportConfig)
	if err != nil {
		return err
	}

	// parse cache kubeconfig
	defaultCacheClientConfig, err := frontProxyKubeconfig.ClientConfig()
	if err != nil {
		return err
	}
	cacheConfig, err := o.Cache.RestConfig(defaultCacheClientConfig)
	if err != nil {
		return err
	}
	cacheKcpClusterClient, err := kcpclientset.NewForConfig(cacheConfig)
	if err != nil {
		return err
	}

	// Don't throttle
	localShardNonIdentityConfig.QPS = -1
	frontProxyNonIdentityConfig.QPS = -1

	u, err := url.Parse(localShardNonIdentityConfig.Host)
	if err != nil {
		return err
	}
	u.Path = ""
	localShardNonIdentityConfig.Host = u.String()

	u, err = url.Parse(frontProxyNonIdentityConfig.Host)
	if err != nil {
		return err
	}
	u.Path = ""
	frontProxyNonIdentityConfig.Host = u.String()

	localShardKubeClusterClient, err := kcpkubernetesclient.NewForConfig(localShardNonIdentityConfig)
	if err != nil {
		return err
	}

	// resolve identities for system APIBindings
	localShardIdentityConfig, resolveIdentities := bootstrap.NewConfigWithWildcardIdentities(localShardNonIdentityConfig, bootstrap.KcpRootGroupExportNames, bootstrap.KcpRootGroupResourceExportNames, localShardKubeClusterClient)
	if err := wait.PollUntilContextCancel(ctx, time.Millisecond*500, true, func(ctx context.Context) (bool, error) {
		if err := resolveIdentities(ctx); err != nil {
			logger.V(3).Info("failed to resolve identities, keeping trying: ", "err", err)
			return false, nil
		}
		return true, nil
	}); err != nil {
		return fmt.Errorf("failed to get or create identities: %w", err)
	}

	// create clients and informers
	localKcpClusterClient, err := kcpclientset.NewForConfig(localShardIdentityConfig)
	if err != nil {
		return err
	}
	cacheFilteredAPIExportClusterClient, err := filteredapiexportclientset.NewForConfig(cacheConfig)
	if err != nil {
		return err
	}

	localKcpInformers := kcpinformers.NewSharedInformerFactory(localKcpClusterClient, 10*time.Minute)
	cacheKcpInformers := kcpinformers.NewSharedInformerFactory(cacheKcpClusterClient, 10*time.Minute)
	filteredAPIExportInformers := filteredapiexportinformers.NewSharedInformerFactory(filteredAPIExportClusterClient, 10*time.Minute)
	cacheFilteredAPIExportInformers := filteredapiexportinformers.NewSharedInformerFactory(cacheFilteredAPIExportClusterClient, 10*time.Minute)

	// create reconciler, install indexers and controllers
	r := reconciler.NewReconciler(
		o.ShardName,
		cacheKcpInformers,
		cacheFilteredAPIExportInformers,
		localKcpInformers,
		filteredAPIExportInformers,
	)
	r.InstallIndexers()
	if err := r.InstallControllers(ctx, localShardIdentityConfig, frontProxyNonIdentityConfig); err != nil {
		return err
	}

	// create apiserver
	scheme := runtime.NewScheme()
	metav1.AddToGroupVersion(scheme, schema.GroupVersion{Group: "", Version: "v1"})
	codecs := serializer.NewCodecFactory(scheme)
	recommendedConfig := genericapiserver.NewRecommendedConfig(codecs)
	if err := o.SecureServing.ApplyTo(&recommendedConfig.Config.SecureServing); err != nil {
		return err
	}
	if err := o.Authentication.ApplyTo(&recommendedConfig.Authentication, recommendedConfig.SecureServing, recommendedConfig.OpenAPIConfig); err != nil {
		return err
	}
	if err := o.Audit.ApplyTo(&recommendedConfig.Config); err != nil {
		return err
	}

	rootAPIServerConfig, err := virtualrootapiserver.NewConfig(recommendedConfig)
	if err != nil {
		return err
	}

	authorizationOptions := virtualoptions.NewAuthorization()
	authorizationOptions.AlwaysAllowGroups = o.Authorization.AlwaysAllowGroups
	authorizationOptions.AlwaysAllowPaths = o.Authorization.AlwaysAllowPaths
	if err := authorizationOptions.ApplyTo(&recommendedConfig.Config, func() []virtualrootapiserver.NamedVirtualWorkspace {
		return rootAPIServerConfig.Extra.VirtualWorkspaces
	}); err != nil {
		return err
	}

	admissionOptions := virtualoptions.NewAdmission()
	if err := admissionOptions.ApplyTo(&recommendedConfig.Config, func() []virtualrootapiserver.NamedVirtualWorkspace {
		return rootAPIServerConfig.Extra.VirtualWorkspaces
	}); err != nil {
		return err
	}

	rootAPIServerConfig.Extra.VirtualWorkspaces, err = o.FilteredAPIExportVirtualWorkspace.NewVirtualWorkspaces(
		o.RootPathPrefix,
		localShardIdentityConfig,
		cacheKcpInformers,
		localKcpInformers,
		filteredAPIExportInformers,
	)
	if err != nil {
		return err
	}

	completedRootAPIServerConfig := rootAPIServerConfig.Complete()
	rootAPIServer, err := virtualrootapiserver.NewServer(completedRootAPIServerConfig, genericapiserver.NewEmptyDelegate())
	if err != nil {
		return err
	}

	preparedRootAPIServer := rootAPIServer.GenericAPIServer.PrepareRun()

	// this **must** be done after PrepareRun() as it sets up the openapi endpoints
	if err := completedRootAPIServerConfig.WithOpenAPIAggregationController(preparedRootAPIServer.GenericAPIServer); err != nil {
		return err
	}

	// Start profiler
	if o.ProfilerAddress != "" {
		//nolint:errcheck,gosec
		go http.ListenAndServe(o.ProfilerAddress, nil)
	}

	logger.Info("Starting informers")
	localKcpInformers.Start(ctx.Done())
	cacheKcpInformers.Start(ctx.Done())
	filteredAPIExportInformers.Start(ctx.Done())
	cacheFilteredAPIExportInformers.Start(ctx.Done())

	localKcpInformers.WaitForCacheSync(ctx.Done())
	cacheKcpInformers.WaitForCacheSync(ctx.Done())
	filteredAPIExportInformers.WaitForCacheSync(ctx.Done())
	cacheFilteredAPIExportInformers.WaitForCacheSync(ctx.Done())

	logger.Info("Starting virtual workspace controllers")
	r.StartControllers(ctx)

	logger.Info("Starting virtual workspace apiserver on ", "externalAddress", rootAPIServerConfig.Generic.ExternalAddress, "version", version.Get().String())
	return preparedRootAPIServer.RunWithContext(ctx)
}

func readKubeConfig(kubeConfigFile, context string) (clientcmd.ClientConfig, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	loadingRules.ExplicitPath = kubeConfigFile

	startingConfig, err := loadingRules.GetStartingConfig()
	if err != nil {
		return nil, err
	}

	overrides := &clientcmd.ConfigOverrides{
		CurrentContext: context,
	}

	clientConfig := clientcmd.NewDefaultClientConfig(*startingConfig, overrides)
	return clientConfig, nil
}
