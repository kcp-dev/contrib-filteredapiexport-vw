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
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/kcp-dev/logicalcluster/v3"
	apisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	conditionsv1alpha1 "github.com/kcp-dev/sdk/apis/third_party/conditions/apis/conditions/v1alpha1"
	"github.com/kcp-dev/sdk/apis/third_party/conditions/util/conditions"
	"github.com/stretchr/testify/require"

	filteredapiexportv1alpha1 "github.com/kcp-dev/contrib-filteredapiexport-vw/sdk/apis/filteredapiexport/v1alpha1"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	testDefaultFilteredAPIExportEndpointSlice = &filteredapiexportv1alpha1.FilteredAPIExportEndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				logicalcluster.AnnotationKey: "root:org:ws",
			},
			Name: "my-slice",
		},
		Spec: filteredapiexportv1alpha1.FilteredAPIExportEndpointSliceSpec{
			APIExport: filteredapiexportv1alpha1.ExportBindingReference{
				Path: "root:org:ws",
				Name: "my-export",
			},
			ObjectSelector: filteredapiexportv1alpha1.FilteredAPIExportObjectSelector{
				LabelSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						"key1": "value1",
					},
				},
			},
		},
	}
)

func TestReconcile(t *testing.T) {
	tests := map[string]struct {
		filteredAPIExportES *filteredapiexportv1alpha1.FilteredAPIExportEndpointSlice

		apiExportMissing     bool
		apiExportInternalErr bool
		errorReason          string

		wantError             bool
		wantAPIExportValid    bool
		wantAPIExportNotValid bool
	}{
		"APIExportValid set to false when APIExport is missing": {
			filteredAPIExportES:   testDefaultFilteredAPIExportEndpointSlice,
			apiExportMissing:      true,
			errorReason:           filteredapiexportv1alpha1.APIExportNotFoundReason,
			wantAPIExportNotValid: true,
		},
		"APIExportValid set to false if an internal error happens when fetching the APIExport": {
			filteredAPIExportES:   testDefaultFilteredAPIExportEndpointSlice,
			apiExportInternalErr:  true,
			wantError:             true,
			errorReason:           filteredapiexportv1alpha1.InternalErrorReason,
			wantAPIExportNotValid: true,
		},
		"APIExportValid set to true when APIExport exists": {
			filteredAPIExportES: testDefaultFilteredAPIExportEndpointSlice,
			wantAPIExportValid:  true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			c := &controller{
				getAPIExport: func(path logicalcluster.Path, name string) (*apisv1alpha2.APIExport, error) {
					if tc.apiExportMissing {
						return nil, apierrors.NewNotFound(apisv1alpha2.Resource("APIExport"), name)
					} else if tc.apiExportInternalErr {
						return nil, fmt.Errorf("internal error")
					}
					return &apisv1alpha2.APIExport{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								logicalcluster.AnnotationKey: "root:org:ws",
							},
							Name: "my-export",
						},
					}, nil
				},
			}

			err := c.reconcile(t.Context(), tc.filteredAPIExportES)
			if tc.wantError {
				require.Error(t, err, "expected an error")
			} else {
				require.NoError(t, err, "expected no error")
			}

			if tc.wantAPIExportNotValid {
				requireConditionMatches(t, tc.filteredAPIExportES,
					conditions.FalseCondition(
						apisv1alpha2.APIExportValid,
						tc.errorReason,
						conditionsv1alpha1.ConditionSeverityError,
						"",
					),
				)
			}

			if tc.wantAPIExportValid {
				requireConditionMatches(t, tc.filteredAPIExportES,
					conditions.TrueCondition(apisv1alpha2.APIExportValid),
				)
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
