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
	"errors"
	"fmt"
	"testing"

	"github.com/kcp-dev/logicalcluster/v3"
	apisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	corev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	conditionsv1alpha1 "github.com/kcp-dev/sdk/apis/third_party/conditions/apis/conditions/v1alpha1"
	"github.com/stretchr/testify/require"

	filteredvwv1alpha1 "github.com/kcp-dev/contrib-filteredapiexport-vw/sdk/apis/filteredvw/v1alpha1"
	filteredvwv1alpha1apply "github.com/kcp-dev/contrib-filteredapiexport-vw/sdk/client/applyconfiguration/filteredvw/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestReconcile(t *testing.T) {
	tests := map[string]struct {
		input               *filteredvwv1alpha1.FilteredAPIExportEndpointSlice
		endpointsReconciler *endpointsReconciler
		expectedError       error
	}{
		"condition not ready": {
			input: &filteredvwv1alpha1.FilteredAPIExportEndpointSlice{
				Status: filteredvwv1alpha1.FilteredAPIExportEndpointSliceStatus{
					Conditions: []conditionsv1alpha1.Condition{
						{
							Type:   filteredvwv1alpha1.APIExportValid,
							Status: corev1.ConditionFalse,
						},
					},
				},
			},
			endpointsReconciler: &endpointsReconciler{},
		},
		"error getting apiExport": {
			input: &filteredvwv1alpha1.FilteredAPIExportEndpointSlice{
				Status: filteredvwv1alpha1.FilteredAPIExportEndpointSliceStatus{
					Conditions: []conditionsv1alpha1.Condition{
						{
							Type:   filteredvwv1alpha1.APIExportValid,
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
		"no shards, no update": {
			input: &filteredvwv1alpha1.FilteredAPIExportEndpointSlice{
				Spec: filteredvwv1alpha1.FilteredAPIExportEndpointSliceSpec{
					APIExport: filteredvwv1alpha1.ExportBindingReference{
						Path: "root:org:ws",
						Name: "my-export",
					},
				},
				Status: filteredvwv1alpha1.FilteredAPIExportEndpointSliceStatus{
					Conditions: []conditionsv1alpha1.Condition{
						{
							Type:   filteredvwv1alpha1.APIExportValid,
							Status: corev1.ConditionTrue,
						},
					},
					FilteredAPIExportEndpoints: []filteredvwv1alpha1.FilteredAPIExportEndpoint{
						{URL: "https://server-1.kcp.dev/old-url"},
					},
				},
			},
			endpointsReconciler: &endpointsReconciler{
				getAPIExport: func(path logicalcluster.Path, name string) (*apisv1alpha2.APIExport, error) {
					return &apisv1alpha2.APIExport{}, nil
				},
				listShards: func() ([]*corev1alpha1.Shard, error) {
					return nil, nil
				},
			},
		},
		"shard without VirtualWorkspaceURL": {
			input: &filteredvwv1alpha1.FilteredAPIExportEndpointSlice{
				Spec: filteredvwv1alpha1.FilteredAPIExportEndpointSliceSpec{
					APIExport: filteredvwv1alpha1.ExportBindingReference{
						Path: "root:org:ws",
						Name: "my-export",
					},
				},
				Status: filteredvwv1alpha1.FilteredAPIExportEndpointSliceStatus{
					Conditions: []conditionsv1alpha1.Condition{
						{
							Type:   filteredvwv1alpha1.APIExportValid,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			endpointsReconciler: &endpointsReconciler{
				getAPIExport: func(path logicalcluster.Path, name string) (*apisv1alpha2.APIExport, error) {
					return &apisv1alpha2.APIExport{}, nil
				},
				listShards: func() ([]*corev1alpha1.Shard, error) {
					return []*corev1alpha1.Shard{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "shard1",
							},
							Spec: corev1alpha1.ShardSpec{
								VirtualWorkspaceURL: "",
							},
						},
					}, nil
				},
			},
		},
		"add urls from shards": {
			input: &filteredvwv1alpha1.FilteredAPIExportEndpointSlice{
				Spec: filteredvwv1alpha1.FilteredAPIExportEndpointSliceSpec{
					APIExport: filteredvwv1alpha1.ExportBindingReference{
						Path: "root:org:ws",
						Name: "my-export",
					},
				},
				Status: filteredvwv1alpha1.FilteredAPIExportEndpointSliceStatus{
					Conditions: []conditionsv1alpha1.Condition{
						{
							Type:   filteredvwv1alpha1.APIExportValid,
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
				listShards: func() ([]*corev1alpha1.Shard, error) {
					return []*corev1alpha1.Shard{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "shard1",
							},
							Spec: corev1alpha1.ShardSpec{
								VirtualWorkspaceURL: "https://server-1.kcp.dev/",
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "shard2",
							},
							Spec: corev1alpha1.ShardSpec{
								VirtualWorkspaceURL: "https://server-2.kcp.dev/",
							},
						},
					}, nil
				},
				patchFilteredAPIExportEndpointSlice: func(ctx context.Context, cluster logicalcluster.Path, patch *filteredvwv1alpha1apply.FilteredAPIExportEndpointSliceApplyConfiguration) error {
					if len(patch.Status.FilteredAPIExportEndpoints) != 2 {
						return fmt.Errorf("unexpected update: expected 2 endpoints, got %d", len(patch.Status.FilteredAPIExportEndpoints))
					}
					url1 := ptr.Deref(patch.Status.FilteredAPIExportEndpoints[0].URL, "")
					url2 := ptr.Deref(patch.Status.FilteredAPIExportEndpoints[1].URL, "")
					if url1 != "https://server-1.kcp.dev/services/filtered-apiexport" {
						return fmt.Errorf("unexpected url1: got %s", url1)
					}
					if url2 != "https://server-2.kcp.dev/services/filtered-apiexport" {
						return fmt.Errorf("unexpected url2: got %s", url2)
					}
					return nil
				},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			c := &controller{
				getAPIExport:                        tc.endpointsReconciler.getAPIExport,
				listShards:                          tc.endpointsReconciler.listShards,
				patchFilteredAPIExportEndpointSlice: tc.endpointsReconciler.patchFilteredAPIExportEndpointSlice,
			}
			input := tc.input.DeepCopy()
			_, err := c.reconcile(t.Context(), input)
			if tc.expectedError != nil {
				require.Error(t, err, tc.expectedError.Error())
			} else {
				require.NoError(t, err, "expected no error")
			}
		})
	}
}
