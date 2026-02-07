/*
Copyright 2025 The KCP Authors.

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

package options

import (
	"fmt"
	"path"

	kcpclientset "github.com/kcp-dev/sdk/client/clientset/versioned/cluster"
	kcpinformers "github.com/kcp-dev/sdk/client/informers/externalversions"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/kcp-dev/contrib-filteredapiexport-vw/internal/virtual/filteredapiexport/builder"
	filteredapiexportinformers "github.com/kcp-dev/contrib-filteredapiexport-vw/sdk/client/informers/externalversions"

	"github.com/kcp-dev/kcp/pkg/authorization"
	"github.com/kcp-dev/kcp/pkg/virtual/framework/rootapiserver"

	kcpkubernetesclientset "github.com/kcp-dev/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type FilteredAPIExport struct {
	KubeconfigFile string
}

func New() *FilteredAPIExport {
	return &FilteredAPIExport{}
}

func (o *FilteredAPIExport) AddFlags(flags *pflag.FlagSet, prefix string) {
	if o == nil {
		return
	}

	flags.StringVar(&o.KubeconfigFile, prefix+"-kubeconfig", o.KubeconfigFile,
		"The kubeconfig file of the KCP APIExport Virtual Workspace that serves FilteredAPIExportEndpointSlices.")
	_ = cobra.MarkFlagRequired(flags, prefix+"-kubeconfig")
}

func (o *FilteredAPIExport) Validate() []error {
	if o == nil {
		return nil
	}
	errs := []error{}

	if o.KubeconfigFile == "" {
		errs = append(errs, fmt.Errorf("kubeconfig is required for the FilteredAPIExport virtual workspace"))
	}

	return errs
}

func (o *FilteredAPIExport) NewVirtualWorkspaces(
	rootPathPrefix string,
	config *rest.Config,
	cachedKcpInformers, wildcardKcpInformers kcpinformers.SharedInformerFactory,
	filteredAPIExportInformers filteredapiexportinformers.SharedInformerFactory,
) (workspaces []rootapiserver.NamedVirtualWorkspace, err error) {
	config = rest.AddUserAgent(rest.CopyConfig(config), "filtered-apiexport-virtual-workspace")
	kcpClusterClient, err := kcpclientset.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	kubeClusterClient, err := kcpkubernetesclientset.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	deepSARClient, err := kcpkubernetesclientset.NewForConfig(authorization.WithDeepSARConfig(rest.CopyConfig(config)))
	if err != nil {
		return nil, err
	}

	return builder.BuildVirtualWorkspace(
		path.Join(rootPathPrefix, builder.VirtualWorkspaceName),
		config,
		kubeClusterClient,
		deepSARClient,
		kcpClusterClient,
		cachedKcpInformers,
		wildcardKcpInformers,
		filteredAPIExportInformers,
	)
}
