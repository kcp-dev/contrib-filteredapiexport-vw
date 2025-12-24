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

package apireconciler

import (
	"fmt"
	"testing"

	"github.com/kcp-dev/logicalcluster/v3"
	apisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	apisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	"github.com/stretchr/testify/require"

	filteredapiexportv1alpha1 "github.com/kcp-dev/contrib-filteredapiexport-vw/sdk/apis/filteredapiexport/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2/ktesting"
)

func TestEnqueueAPIResourceSchema(t *testing.T) {
	c := &APIReconciler{
		queue: workqueue.NewTypedRateLimitingQueueWithConfig(
			workqueue.DefaultTypedControllerRateLimiter[string](),
			workqueue.TypedRateLimitingQueueConfig[string]{
				Name: ControllerName,
			},
		),
		listAPIExports: func(clusterName logicalcluster.Name) ([]*apisv1alpha2.APIExport, error) {
			return []*apisv1alpha2.APIExport{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "export1",
					},
					Spec: apisv1alpha2.APIExportSpec{
						Resources: []apisv1alpha2.ResourceSchema{
							{
								Name: "schema1",
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "export2",
					},
					Spec: apisv1alpha2.APIExportSpec{
						Resources: []apisv1alpha2.ResourceSchema{
							{
								Name: "schema2",
							},
						},
					},
				},
			}, nil
		},
		getAPIExport: func(clusterName logicalcluster.Name, name string) (*apisv1alpha2.APIExport, error) {
			return &apisv1alpha2.APIExport{
				ObjectMeta: metav1.ObjectMeta{
					Name: name,
				},
				Spec: apisv1alpha2.APIExportSpec{
					Resources: []apisv1alpha2.ResourceSchema{
						{
							Schema: fmt.Sprintf("schema%s", name[len(name)-1:]),
						},
					},
				},
			}, nil
		},
		listFilteredAPIExportEndpointSlices: func(clusterName logicalcluster.Name) ([]*filteredapiexportv1alpha1.FilteredAPIExportEndpointSlice, error) {
			return []*filteredapiexportv1alpha1.FilteredAPIExportEndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "filteredapiexportes1",
					},
					Spec: filteredapiexportv1alpha1.FilteredAPIExportEndpointSliceSpec{
						APIExport: filteredapiexportv1alpha1.ExportBindingReference{
							Name: "export1",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "filteredapiexportes2",
					},
					Spec: filteredapiexportv1alpha1.FilteredAPIExportEndpointSliceSpec{
						APIExport: filteredapiexportv1alpha1.ExportBindingReference{
							Name: "export2",
						},
					},
				},
			}, nil
		},
	}

	schema := &apisv1alpha1.APIResourceSchema{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				logicalcluster.AnnotationKey: "mycluster",
			},
			Name: "schema1",
		},
	}

	logger, _ := ktesting.NewTestContext(t)
	c.enqueueAPIResourceSchema(schema, logger)

	require.Equal(t, c.queue.Len(), 1)

	// get the queue keys
	actual := sets.New[string]()
	item, _ := c.queue.Get()
	actual.Insert(item)

	expected := sets.New("filteredapiexportes1")
	require.True(t, expected.Equal(actual))
}
