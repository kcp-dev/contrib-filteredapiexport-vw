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

	filteredapiexportbuilder "github.com/kcp-dev/contrib-filteredapiexport-vw/internal/virtual/filteredapiexport/builder"
	filteredvwv1alpha1 "github.com/kcp-dev/contrib-filteredapiexport-vw/sdk/apis/filteredvw/v1alpha1"
	filteredvwv1alpha1apply "github.com/kcp-dev/contrib-filteredapiexport-vw/sdk/client/applyconfiguration/filteredvw/v1alpha1"

	"github.com/kcp-dev/kcp/pkg/logging"

	"k8s.io/klog/v2"
)

// DefaultRootPathPrefix is basically constant forever, or we risk a breaking change. The
// kubectl plugin for example will use this prefix to generate the root path, and because
// we don't control kubectl plugin updates, we cannot change this prefix.
const DefaultRootPathPrefix string = "/services"

type endpointsReconciler struct {
	getMyShard                          func() (*corev1alpha1.Shard, error)
	getAPIExport                        func(path logicalcluster.Path, name string) (*apisv1alpha2.APIExport, error)
	listShards                          func() ([]*corev1alpha1.Shard, error)
	listAPIBindingsByAPIExport          func(apiexport *apisv1alpha2.APIExport) ([]*apisv1alpha2.APIBinding, error)
	patchFilteredAPIExportEndpointSlice func(ctx context.Context, cluster logicalcluster.Path, patch *filteredvwv1alpha1apply.FilteredAPIExportEndpointSliceApplyConfiguration) error
}

type result struct {
	urls []string
}

func (c *controller) reconcile(ctx context.Context, filteredAPIExportEndpointSlice *filteredvwv1alpha1.FilteredAPIExportEndpointSlice) (bool, error) {
	r := &endpointsReconciler{
		getMyShard:                          c.getMyShard,
		getAPIExport:                        c.getAPIExport,
		listShards:                          c.listShards,
		listAPIBindingsByAPIExport:          c.listAPIBindingsByAPIExport,
		patchFilteredAPIExportEndpointSlice: c.patchFilteredAPIExportEndpointSlice,
	}

	return r.reconcile(ctx, filteredAPIExportEndpointSlice)
}

func (r *endpointsReconciler) reconcile(ctx context.Context, filteredAPIExportEndpointSlice *filteredvwv1alpha1.FilteredAPIExportEndpointSlice) (bool, error) {
	// we only continue if all conditions are set to true. As this is more of the secondary controller,
	// we don't want to do anything if the primary controller is not ready.
	for _, condition := range filteredAPIExportEndpointSlice.Status.Conditions {
		if !conditions.IsTrue(filteredAPIExportEndpointSlice, condition.Type) {
			return false, nil
		}
	}

	apiExportPath := logicalcluster.NewPath(filteredAPIExportEndpointSlice.Spec.APIExport.Path)
	if apiExportPath.Empty() {
		apiExportPath = logicalcluster.From(filteredAPIExportEndpointSlice).Path()
	}
	apiExport, err := r.getAPIExport(apiExportPath, filteredAPIExportEndpointSlice.Spec.APIExport.Name)
	if err != nil {
		return true, err
	}

	allShards, err := r.listShards()
	if err != nil {
		return true, err
	}

	results, err := r.updateEndpoints(ctx, filteredAPIExportEndpointSlice, apiExport, allShards)
	if err != nil {
		return true, err
	}
	if results == nil { // no change, nothing to do.
		return false, nil
	}

	// Patch the object
	patch := filteredvwv1alpha1apply.FilteredAPIExportEndpointSlice(filteredAPIExportEndpointSlice.Name)
	urlPatches := []*filteredvwv1alpha1apply.FilteredAPIExportEndpointApplyConfiguration{}
	for _, rs := range results.urls {
		urlPatches = append(urlPatches, filteredvwv1alpha1apply.FilteredAPIExportEndpoint().WithURL(rs))
	}
	patch.WithStatus(filteredvwv1alpha1apply.FilteredAPIExportEndpointSliceStatus().
		WithFilteredAPIExportEndpoints(urlPatches...))
	cluster := logicalcluster.From(filteredAPIExportEndpointSlice)
	err = r.patchFilteredAPIExportEndpointSlice(ctx, cluster.Path(), patch)
	if err != nil {
		return true, err
	}

	return false, nil
}

func (r *endpointsReconciler) updateEndpoints(ctx context.Context,
	filteredAPIExportEndpointSlice *filteredvwv1alpha1.FilteredAPIExportEndpointSlice,
	apiExport *apisv1alpha2.APIExport,
	shards []*corev1alpha1.Shard,
) (*result, error) {
	logger := klog.FromContext(ctx)
	results := &result{}
	results.urls = []string{}

	for _, shard := range shards {
		if shard.Spec.VirtualWorkspaceURL == "" {
			return nil, nil
		}

		if filteredAPIExportEndpointSlice.Status.FilteredAPIExportEndpoints == nil {
			filteredAPIExportEndpointSlice.Status.FilteredAPIExportEndpoints = []filteredvwv1alpha1.FilteredAPIExportEndpoint{}
		}

		u, err := url.Parse(shard.Spec.VirtualWorkspaceURL)
		if err != nil {
			// Should never happen
			logger = logging.WithObject(logger, shard)
			logger.Error(
				err, "error parsing shard.spec.virtualWorkspaceURL",
				"VirtualWorkspaceURL", shard.Spec.VirtualWorkspaceURL,
			)
			return nil, nil
		}

		u.Path = path.Join(
			u.Path,
			DefaultRootPathPrefix,
			filteredapiexportbuilder.VirtualWorkspaceName,
			logicalcluster.From(apiExport).String(),
			filteredAPIExportEndpointSlice.Name,
		)

		for _, existingURL := range filteredAPIExportEndpointSlice.Status.FilteredAPIExportEndpoints {
			if existingURL.URL == u.String() {
				continue
			}
		}

		results.urls = append(results.urls, u.String())
	}

	if len(results.urls) == 0 {
		return nil, nil
	}

	return results, nil
}
