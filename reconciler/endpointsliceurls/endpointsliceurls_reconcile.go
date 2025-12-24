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

package endpointsliceurls

import (
	"context"
	"net/url"
	"path"

	"github.com/kcp-dev/logicalcluster/v3"
	apisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	corev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	"github.com/kcp-dev/sdk/apis/third_party/conditions/util/conditions"

	filteredapiexportv1alpha1 "github.com/kcp-dev/contrib-filteredapiexport-vw/sdk/apis/filteredapiexport/v1alpha1"

	virtualworkspacesoptions "github.com/kcp-dev/kcp/cmd/virtual-workspaces/options"
	"github.com/kcp-dev/kcp/pkg/logging"
	apiexportbuilder "github.com/kcp-dev/kcp/pkg/virtual/apiexport/builder"

	"k8s.io/klog/v2"
)

type endpointsReconciler struct {
	getMyShard                 func() (*corev1alpha1.Shard, error)
	getAPIExport               func(path logicalcluster.Path, name string) (*apisv1alpha2.APIExport, error)
	listAPIBindingsByAPIExport func(apiexport *apisv1alpha2.APIExport) ([]*apisv1alpha2.APIBinding, error)
}

func (c *controller) reconcile(ctx context.Context, filteredAPIExportEndpointSlice *filteredapiexportv1alpha1.FilteredAPIExportEndpointSlice) error {
	r := &endpointsReconciler{
		getMyShard:                 c.getMyShard,
		getAPIExport:               c.getAPIExport,
		listAPIBindingsByAPIExport: c.listAPIBindingsByAPIExport,
	}

	return r.reconcile(ctx, filteredAPIExportEndpointSlice)
}

func (r *endpointsReconciler) reconcile(ctx context.Context, filteredAPIExportEndpointSlice *filteredapiexportv1alpha1.FilteredAPIExportEndpointSlice) error {
	// we only continue if all conditions are set to true. As this is more of the secondary controller,
	// we don't want to do anything if the primary controller is not ready.
	for _, condition := range filteredAPIExportEndpointSlice.Status.Conditions {
		if !conditions.IsTrue(filteredAPIExportEndpointSlice, condition.Type) {
			return nil
		}
	}

	apiExportPath := logicalcluster.NewPath(filteredAPIExportEndpointSlice.Spec.APIExport.Path)
	if apiExportPath.Empty() {
		apiExportPath = logicalcluster.From(filteredAPIExportEndpointSlice).Path()
	}
	apiExport, err := r.getAPIExport(apiExportPath, filteredAPIExportEndpointSlice.Spec.APIExport.Name)
	if err != nil {
		return err
	}

	shard, err := r.getMyShard()
	if err != nil {
		return err
	}

	r.updateEndpoints(ctx, filteredAPIExportEndpointSlice, apiExport, shard)

	return nil
}

func (r *endpointsReconciler) updateEndpoints(ctx context.Context,
	filteredAPIExportEndpointSlice *filteredapiexportv1alpha1.FilteredAPIExportEndpointSlice,
	apiExport *apisv1alpha2.APIExport,
	shard *corev1alpha1.Shard,
) {
	logger := klog.FromContext(ctx)
	if shard.Spec.VirtualWorkspaceURL == "" {
		return
	}

	// Check if we have local consumers
	bindings, err := r.listAPIBindingsByAPIExport(apiExport)
	if err != nil {
		return
	}

	if len(bindings) == 0 { // we have no consumers, remove endpoint
		filteredAPIExportEndpointSlice.Status.FilteredAPIExportEndpoints = nil
		return
	}

	u, err := url.Parse(shard.Spec.VirtualWorkspaceURL)
	if err != nil {
		// Should never happen
		logger = logging.WithObject(logger, shard)
		logger.Error(
			err, "error parsing shard.spec.virtualWorkspaceURL",
			"VirtualWorkspaceURL", shard.Spec.VirtualWorkspaceURL,
		)
		return
	}

	u.Path = path.Join(
		u.Path,
		virtualworkspacesoptions.DefaultRootPathPrefix,
		apiexportbuilder.VirtualWorkspaceName,
		logicalcluster.From(apiExport).String(),
		apiExport.Name,
	)

	endpointURL := u.String()

	if filteredAPIExportEndpointSlice.Status.FilteredAPIExportEndpoints == nil {
		filteredAPIExportEndpointSlice.Status.FilteredAPIExportEndpoints = []filteredapiexportv1alpha1.FilteredAPIExportEndpoint{}
	}

	// Check if already set
	for _, ep := range filteredAPIExportEndpointSlice.Status.FilteredAPIExportEndpoints {
		if ep.URL == endpointURL {
			return
		}
	}

	// Add the endpoint
	filteredAPIExportEndpointSlice.Status.FilteredAPIExportEndpoints = append(filteredAPIExportEndpointSlice.Status.FilteredAPIExportEndpoints,
		filteredapiexportv1alpha1.FilteredAPIExportEndpoint{
			URL: endpointURL,
		},
	)
}
