/*
Copyright 2022 The KCP Authors.

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

package endpointslice

import (
	"context"

	"github.com/kcp-dev/logicalcluster/v3"
	apisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	conditionsv1alpha1 "github.com/kcp-dev/sdk/apis/third_party/conditions/apis/conditions/v1alpha1"
	"github.com/kcp-dev/sdk/apis/third_party/conditions/util/conditions"

	filteredvwv1alpha1 "github.com/kcp-dev/contrib-filteredapiexport-vw/sdk/apis/filteredvw/v1alpha1"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

type endpointsReconciler struct {
	getAPIExport func(path logicalcluster.Path, name string) (*apisv1alpha2.APIExport, error)
}

func (c *controller) reconcile(ctx context.Context, filteredAPIExportES *filteredvwv1alpha1.FilteredAPIExportEndpointSlice) error {
	r := &endpointsReconciler{
		getAPIExport: c.getAPIExport,
	}

	return r.reconcile(ctx, filteredAPIExportES)
}

func (r *endpointsReconciler) reconcile(_ context.Context, filteredAPIExportES *filteredvwv1alpha1.FilteredAPIExportEndpointSlice) error {
	// Get APIExport
	apiExportPath := logicalcluster.NewPath(filteredAPIExportES.Spec.APIExport.Path)
	if apiExportPath.Empty() {
		apiExportPath = logicalcluster.From(filteredAPIExportES).Path()
	}
	_, err := r.getAPIExport(apiExportPath, filteredAPIExportES.Spec.APIExport.Name)
	if err != nil {
		reason := filteredvwv1alpha1.InternalErrorReason
		if apierrors.IsNotFound(err) {
			// Don't keep the endpoints if the APIExport has been deleted
			conditions.MarkFalse(
				filteredAPIExportES,
				filteredvwv1alpha1.APIExportValid,
				filteredvwv1alpha1.APIExportNotFoundReason,
				conditionsv1alpha1.ConditionSeverityError,
				"Error getting APIExport %s|%s",
				apiExportPath,
				filteredAPIExportES.Spec.APIExport.Name,
			)
			// No need to try again
			return nil
		} else {
			conditions.MarkFalse(
				filteredAPIExportES,
				filteredvwv1alpha1.APIExportValid,
				reason,
				conditionsv1alpha1.ConditionSeverityError,
				"Error getting APIExport %s|%s",
				apiExportPath,
				filteredAPIExportES.Spec.APIExport.Name,
			)
			return err
		}
	}

	conditions.MarkTrue(filteredAPIExportES, filteredvwv1alpha1.APIExportValid)

	return nil
}
