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
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/kcp-dev/logicalcluster/v3"
	apisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	corev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	conditionsv1alpha1 "github.com/kcp-dev/sdk/apis/third_party/conditions/apis/conditions/v1alpha1"
	"github.com/kcp-dev/sdk/apis/third_party/conditions/util/conditions"
	"github.com/stretchr/testify/require"

	filteredapiexportv1alpha1 "github.com/kcp-dev/contrib-filteredapiexport-vw/sdk/apis/filteredapiexport/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestReconcile(t *testing.T) {
	tests := map[string]struct {
		input               *filteredapiexportv1alpha1.FilteredAPIExportEndpointSlice
		endpointsReconciler *endpointsReconciler
		expectedConditions  []*conditionsv1alpha1.Condition
		expectedEndpoints   []filteredapiexportv1alpha1.FilteredAPIExportEndpoint
		expectedError       error
	}{
		"condition not ready": {
			input: &filteredapiexportv1alpha1.FilteredAPIExportEndpointSlice{
				Status: filteredapiexportv1alpha1.FilteredAPIExportEndpointSliceStatus{
					Conditions: []conditionsv1alpha1.Condition{
						{
							Type:   filteredapiexportv1alpha1.APIExportValid,
							Status: corev1.ConditionFalse,
						},
					},
				},
			},
			endpointsReconciler: &endpointsReconciler{},
		},
		"error getting apiExport": {
			input: &filteredapiexportv1alpha1.FilteredAPIExportEndpointSlice{
				Status: filteredapiexportv1alpha1.FilteredAPIExportEndpointSliceStatus{
					Conditions: []conditionsv1alpha1.Condition{
						{
							Type:   filteredapiexportv1alpha1.APIExportValid,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			endpointsReconciler: &endpointsReconciler{
				getAPIExport: func(path logicalcluster.Path, name string) (*apisv1alpha2.APIExport, error) {
					return nil, errors.New("lost in space")
				},
			},
			expectedError: errors.New("lost in space"),
		},
		"no consumers, remove url": {
			input: &filteredapiexportv1alpha1.FilteredAPIExportEndpointSlice{
				Spec: filteredapiexportv1alpha1.FilteredAPIExportEndpointSliceSpec{
					APIExport: filteredapiexportv1alpha1.ExportBindingReference{
						Path: "root:org:ws",
						Name: "my-export",
					},
				},
				Status: filteredapiexportv1alpha1.FilteredAPIExportEndpointSliceStatus{
					Conditions: []conditionsv1alpha1.Condition{
						{
							Type:   filteredapiexportv1alpha1.APIExportValid,
							Status: corev1.ConditionTrue,
						},
					},
					FilteredAPIExportEndpoints: []filteredapiexportv1alpha1.FilteredAPIExportEndpoint{
						{URL: "https://server-1.kcp.dev/old-url"},
					},
				},
			},
			endpointsReconciler: &endpointsReconciler{
				getAPIExport: func(path logicalcluster.Path, name string) (*apisv1alpha2.APIExport, error) {
					return &apisv1alpha2.APIExport{}, nil
				},
				getMyShard: func() (*corev1alpha1.Shard, error) {
					return &corev1alpha1.Shard{
						ObjectMeta: metav1.ObjectMeta{
							Name: "shard1",
						},
						Spec: corev1alpha1.ShardSpec{
							VirtualWorkspaceURL: "https://server-1.kcp.dev/",
						},
					}, nil
				},
				listAPIBindingsByAPIExport: func(apiexport *apisv1alpha2.APIExport) ([]*apisv1alpha2.APIBinding, error) {
					return nil, nil
				},
			},
			expectedEndpoints: nil, // endpoints should be removed
		},
		"consumer went away, remove url": {
			input: &filteredapiexportv1alpha1.FilteredAPIExportEndpointSlice{
				Spec: filteredapiexportv1alpha1.FilteredAPIExportEndpointSliceSpec{
					APIExport: filteredapiexportv1alpha1.ExportBindingReference{
						Path: "root:org:ws",
						Name: "my-export",
					},
				},
				Status: filteredapiexportv1alpha1.FilteredAPIExportEndpointSliceStatus{
					Conditions: []conditionsv1alpha1.Condition{
						{
							Type:   filteredapiexportv1alpha1.APIExportValid,
							Status: corev1.ConditionTrue,
						},
					},
					FilteredAPIExportEndpoints: []filteredapiexportv1alpha1.FilteredAPIExportEndpoint{
						{
							URL: "https://server-1.kcp.dev/who-took-the-cookie-from-the-cookie-jar",
						},
					},
				},
			},
			endpointsReconciler: &endpointsReconciler{
				getAPIExport: func(path logicalcluster.Path, name string) (*apisv1alpha2.APIExport, error) {
					return &apisv1alpha2.APIExport{}, nil
				},
				getMyShard: func() (*corev1alpha1.Shard, error) {
					return &corev1alpha1.Shard{
						ObjectMeta: metav1.ObjectMeta{
							Name: "shard1",
						},
						Spec: corev1alpha1.ShardSpec{
							VirtualWorkspaceURL: "https://server-1.kcp.dev/",
						},
					}, nil
				},
				listAPIBindingsByAPIExport: func(apiexport *apisv1alpha2.APIExport) ([]*apisv1alpha2.APIBinding, error) {
					return nil, nil
				},
			},
			expectedEndpoints: nil, // endpoints should be removed
		},
		"consumer exists, add url": {
			input: &filteredapiexportv1alpha1.FilteredAPIExportEndpointSlice{
				Spec: filteredapiexportv1alpha1.FilteredAPIExportEndpointSliceSpec{
					APIExport: filteredapiexportv1alpha1.ExportBindingReference{
						Path: "root:org:ws",
						Name: "my-export",
					},
				},
				Status: filteredapiexportv1alpha1.FilteredAPIExportEndpointSliceStatus{
					Conditions: []conditionsv1alpha1.Condition{
						{
							Type:   filteredapiexportv1alpha1.APIExportValid,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			endpointsReconciler: &endpointsReconciler{
				getAPIExport: func(path logicalcluster.Path, name string) (*apisv1alpha2.APIExport, error) {
					return &apisv1alpha2.APIExport{
						ObjectMeta: metav1.ObjectMeta{
							Name: "my-export",
						},
					}, nil
				},
				getMyShard: func() (*corev1alpha1.Shard, error) {
					return &corev1alpha1.Shard{
						ObjectMeta: metav1.ObjectMeta{
							Name: "shard1",
						},
						Spec: corev1alpha1.ShardSpec{
							VirtualWorkspaceURL: "https://server-1.kcp.dev/",
						},
					}, nil
				},
				listAPIBindingsByAPIExport: func(apiexport *apisv1alpha2.APIExport) ([]*apisv1alpha2.APIBinding, error) {
					return []*apisv1alpha2.APIBinding{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "my-binding",
							},
						},
					}, nil
				},
			},
			expectedEndpoints: []filteredapiexportv1alpha1.FilteredAPIExportEndpoint{
				{URL: "https://server-1.kcp.dev/services/apiexport/my-export"},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			c := &controller{
				getMyShard:                 tc.endpointsReconciler.getMyShard,
				getAPIExport:               tc.endpointsReconciler.getAPIExport,
				listAPIBindingsByAPIExport: tc.endpointsReconciler.listAPIBindingsByAPIExport,
			}
			input := tc.input.DeepCopy()
			err := c.reconcile(t.Context(), input)
			if tc.expectedError != nil {
				require.Error(t, err, tc.expectedError.Error())
			} else {
				require.NoError(t, err, "expected no error")
			}

			for _, expectedCondition := range tc.expectedConditions {
				requireConditionMatches(t, input, expectedCondition)
			}

			if tc.expectedEndpoints != nil || len(input.Status.FilteredAPIExportEndpoints) > 0 {
				if diff := cmp.Diff(tc.expectedEndpoints, input.Status.FilteredAPIExportEndpoints); diff != "" {
					t.Errorf("unexpected endpoints (-want +got):\n%s", diff)
				}
			}
		})
	}
}

// requireConditionMatches looks for a condition matching c in g. LastTransitionTime and Message
// are not compared.
func requireConditionMatches(t *testing.T, g conditions.Getter, c *conditionsv1alpha1.Condition) {
	t.Helper()
	actual := conditions.Get(g, c.Type)
	require.NotNil(t, actual, "missing condition %q", c.Type)
	actual.LastTransitionTime = c.LastTransitionTime
	actual.Message = c.Message
	require.Empty(t, cmp.Diff(actual, c))
}
