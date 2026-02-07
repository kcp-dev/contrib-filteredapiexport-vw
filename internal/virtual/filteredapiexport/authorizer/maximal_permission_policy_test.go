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

package authorizer

import (
	"context"
	"testing"

	"github.com/kcp-dev/logicalcluster/v3"
	apisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	"github.com/stretchr/testify/require"

	filteredvwv1alpha1 "github.com/kcp-dev/contrib-filteredapiexport-vw/sdk/apis/filteredvw/v1alpha1"

	dynamiccontext "github.com/kcp-dev/kcp/pkg/virtual/framework/dynamic/context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/authorization/authorizer"
)

func TestMaximalPermissionPolicyAuthorizer(t *testing.T) {
	for _, tc := range []struct {
		name                              string
		attr                              authorizer.Attributes
		apidomainKey                      string
		getFilteredAPIExportEndpointSlice func(clusterName, filteredAPIExportESName string) (*filteredvwv1alpha1.FilteredAPIExportEndpointSlice, error)
		getAPIExport                      func(clusterName, apiExportName string) (*apisv1alpha2.APIExport, error)
		getAPIExportsByIdentity           func(identityHash string) ([]*apisv1alpha2.APIExport, error)
		newDeepSARAuthorizer              func(clusterName logicalcluster.Name) (authorizer.Authorizer, error)

		expectedErr      string
		expectedDecision authorizer.Decision
		expectedReason   string
	}{
		{
			name:             "invalid domain key",
			attr:             &authorizer.AttributesRecord{User: &user.DefaultInfo{}},
			apidomainKey:     "",
			expectedDecision: authorizer.DecisionNoOpinion,
			expectedErr:      "invalid API domain key",
		},
		{
			name:         "no claimed identities",
			attr:         &authorizer.AttributesRecord{User: &user.DefaultInfo{}},
			apidomainKey: "foo/bar",
			getAPIExport: func(clusterName, apiExportName string) (*apisv1alpha2.APIExport, error) {
				return &apisv1alpha2.APIExport{
					ObjectMeta: metav1.ObjectMeta{
						Name: "fooExport",
						Annotations: map[string]string{
							logicalcluster.AnnotationKey: "someWorkspace",
						},
					},
				}, nil
			},
			getFilteredAPIExportEndpointSlice: func(clusterName, filteredAPIExportESName string) (*filteredvwv1alpha1.FilteredAPIExportEndpointSlice, error) {
				return &filteredvwv1alpha1.FilteredAPIExportEndpointSlice{
					ObjectMeta: metav1.ObjectMeta{
						Name: "fooFilteredSlice",
					},
					Spec: filteredvwv1alpha1.FilteredAPIExportEndpointSliceSpec{
						APIExport: filteredvwv1alpha1.ExportBindingReference{
							Name: "fooExport",
						},
					},
				}, nil
			},
			expectedDecision: authorizer.DecisionAllow,
			expectedReason:   `unclaimed resource in APIExport: "fooExport", workspace :"someWorkspace"`,
		},
		{
			name: "claimed identity without identity hash",
			attr: &authorizer.AttributesRecord{
				User:     &user.DefaultInfo{},
				APIGroup: "claimedGroup",
				Resource: "claimedResource",
			},
			apidomainKey: "foo/bar",
			getAPIExport: func(clusterName, apiExportName string) (*apisv1alpha2.APIExport, error) {
				return &apisv1alpha2.APIExport{
					ObjectMeta: metav1.ObjectMeta{
						Name: "fooExport",
						Annotations: map[string]string{
							logicalcluster.AnnotationKey: "someWorkspace",
						},
					},
					Spec: apisv1alpha2.APIExportSpec{
						PermissionClaims: []apisv1alpha2.PermissionClaim{
							{
								GroupResource: apisv1alpha2.GroupResource{
									Group:    "someGroup",
									Resource: "someResource",
								},
							},
							{
								GroupResource: apisv1alpha2.GroupResource{
									Group:    "claimedGroup",
									Resource: "claimedResource",
								},
							},
						},
					},
				}, nil
			},
			getFilteredAPIExportEndpointSlice: func(clusterName, filteredAPIExportESName string) (*filteredvwv1alpha1.FilteredAPIExportEndpointSlice, error) {
				return &filteredvwv1alpha1.FilteredAPIExportEndpointSlice{
					ObjectMeta: metav1.ObjectMeta{
						Name: "fooFilteredSlice",
					},
					Spec: filteredvwv1alpha1.FilteredAPIExportEndpointSliceSpec{
						APIExport: filteredvwv1alpha1.ExportBindingReference{
							Name: "fooExport",
						},
					},
				}, nil
			},
			expectedDecision: authorizer.DecisionAllow,
			expectedReason:   `unclaimable resource, identity hash not set in claiming APIExport: "fooExport", workspace :"someWorkspace"`,
		},
		{
			name: "claimed identity without api export",
			attr: &authorizer.AttributesRecord{
				User:     &user.DefaultInfo{},
				APIGroup: "claimedGroup",
				Resource: "claimedResource",
			},
			apidomainKey: "foo/bar",
			getAPIExport: func(clusterName, apiExportName string) (*apisv1alpha2.APIExport, error) {
				return &apisv1alpha2.APIExport{
					ObjectMeta: metav1.ObjectMeta{
						Name: "fooExport",
						Annotations: map[string]string{
							logicalcluster.AnnotationKey: "someWorkspace",
						},
					},
					Spec: apisv1alpha2.APIExportSpec{
						PermissionClaims: []apisv1alpha2.PermissionClaim{
							{
								GroupResource: apisv1alpha2.GroupResource{
									Group:    "someGroup",
									Resource: "someResource",
								},
							},
							{
								GroupResource: apisv1alpha2.GroupResource{
									Group:    "claimedGroup",
									Resource: "claimedResource",
								},
								IdentityHash: "123",
							},
						},
					},
				}, nil
			},
			getAPIExportsByIdentity: func(identityHash string) ([]*apisv1alpha2.APIExport, error) {
				return []*apisv1alpha2.APIExport{}, nil
			},
			getFilteredAPIExportEndpointSlice: func(clusterName, filteredAPIExportESName string) (*filteredvwv1alpha1.FilteredAPIExportEndpointSlice, error) {
				return &filteredvwv1alpha1.FilteredAPIExportEndpointSlice{
					ObjectMeta: metav1.ObjectMeta{
						Name: "fooFilteredSlice",
					},
					Spec: filteredvwv1alpha1.FilteredAPIExportEndpointSliceSpec{
						APIExport: filteredvwv1alpha1.ExportBindingReference{
							Name: "fooExport",
						},
					},
				}, nil
			},
			expectedDecision: authorizer.DecisionDeny,
			expectedReason:   `no APIExport providing claimed resources found for identity hash: "123"`,
		},
		{
			name: "claimed identity with api export having no maximum permission policy",
			attr: &authorizer.AttributesRecord{
				User:     &user.DefaultInfo{},
				APIGroup: "claimedGroup",
				Resource: "claimedResource",
			},
			apidomainKey: "foo/bar",
			getAPIExport: func(clusterName, apiExportName string) (*apisv1alpha2.APIExport, error) {
				return &apisv1alpha2.APIExport{
					ObjectMeta: metav1.ObjectMeta{
						Name: "fooExport",
						Annotations: map[string]string{
							logicalcluster.AnnotationKey: "someWorkspace",
						},
					},
					Spec: apisv1alpha2.APIExportSpec{
						PermissionClaims: []apisv1alpha2.PermissionClaim{
							{
								GroupResource: apisv1alpha2.GroupResource{
									Group:    "someGroup",
									Resource: "someResource",
								},
							},
							{
								GroupResource: apisv1alpha2.GroupResource{
									Group:    "claimedGroup",
									Resource: "claimedResource",
								},
								IdentityHash: "123",
							},
						},
					},
				}, nil
			},
			getAPIExportsByIdentity: func(identityHash string) ([]*apisv1alpha2.APIExport, error) {
				return []*apisv1alpha2.APIExport{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "foo",
						},
					},
				}, nil
			},
			getFilteredAPIExportEndpointSlice: func(clusterName, filteredAPIExportESName string) (*filteredvwv1alpha1.FilteredAPIExportEndpointSlice, error) {
				return &filteredvwv1alpha1.FilteredAPIExportEndpointSlice{
					ObjectMeta: metav1.ObjectMeta{
						Name: "fooFilteredSlice",
					},
					Spec: filteredvwv1alpha1.FilteredAPIExportEndpointSliceSpec{
						APIExport: filteredvwv1alpha1.ExportBindingReference{
							Name: "fooExport",
						},
					},
				}, nil
			},
			expectedDecision: authorizer.DecisionAllow,
			expectedReason:   `all claimed APIExports granted access`,
		},
		{
			name: "claimed identity with api export having maximum permission policy granting access",
			attr: &authorizer.AttributesRecord{
				User:     &user.DefaultInfo{},
				APIGroup: "claimedGroup",
				Resource: "claimedResource",
			},
			apidomainKey: "foo/bar",
			getAPIExport: func(clusterName, apiExportName string) (*apisv1alpha2.APIExport, error) {
				return &apisv1alpha2.APIExport{
					ObjectMeta: metav1.ObjectMeta{
						Name: "fooExport",
						Annotations: map[string]string{
							logicalcluster.AnnotationKey: "someWorkspace",
						},
					},
					Spec: apisv1alpha2.APIExportSpec{
						PermissionClaims: []apisv1alpha2.PermissionClaim{
							{
								GroupResource: apisv1alpha2.GroupResource{
									Group:    "someGroup",
									Resource: "someResource",
								},
							},
							{
								GroupResource: apisv1alpha2.GroupResource{
									Group:    "claimedGroup",
									Resource: "claimedResource",
								},
								IdentityHash: "123",
							},
						},
					},
				}, nil
			},
			getAPIExportsByIdentity: func(identityHash string) ([]*apisv1alpha2.APIExport, error) {
				return []*apisv1alpha2.APIExport{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "foo",
						},
						Spec: apisv1alpha2.APIExportSpec{
							MaximalPermissionPolicy: &apisv1alpha2.MaximalPermissionPolicy{Local: &apisv1alpha2.LocalAPIExportPolicy{}},
						},
					},
				}, nil
			},
			newDeepSARAuthorizer: func(clusterName logicalcluster.Name) (authorizer.Authorizer, error) {
				return authorizer.AuthorizerFunc(func(ctx context.Context, a authorizer.Attributes) (authorizer.Decision, string, error) {
					return authorizer.DecisionAllow, "", nil
				}), nil
			},
			getFilteredAPIExportEndpointSlice: func(clusterName, filteredAPIExportESName string) (*filteredvwv1alpha1.FilteredAPIExportEndpointSlice, error) {
				return &filteredvwv1alpha1.FilteredAPIExportEndpointSlice{
					ObjectMeta: metav1.ObjectMeta{
						Name: "fooFilteredSlice",
					},
					Spec: filteredvwv1alpha1.FilteredAPIExportEndpointSliceSpec{
						APIExport: filteredvwv1alpha1.ExportBindingReference{
							Name: "fooExport",
						},
					},
				}, nil
			},
			expectedDecision: authorizer.DecisionAllow,
			expectedReason:   `all claimed APIExports granted access`,
		},
		{
			name: "claimed identity with api export having maximum permission policy denying access",
			attr: &authorizer.AttributesRecord{
				User:     &user.DefaultInfo{},
				APIGroup: "claimedGroup",
				Resource: "claimedResource",
			},
			apidomainKey: "foo/bar",
			getAPIExport: func(clusterName, apiExportName string) (*apisv1alpha2.APIExport, error) {
				return &apisv1alpha2.APIExport{
					ObjectMeta: metav1.ObjectMeta{
						Name: "fooExport",
						Annotations: map[string]string{
							logicalcluster.AnnotationKey: "someWorkspace",
						},
					},
					Spec: apisv1alpha2.APIExportSpec{
						PermissionClaims: []apisv1alpha2.PermissionClaim{
							{
								GroupResource: apisv1alpha2.GroupResource{
									Group:    "someGroup",
									Resource: "someResource",
								},
							},
							{
								GroupResource: apisv1alpha2.GroupResource{
									Group:    "claimedGroup",
									Resource: "claimedResource",
								},
								IdentityHash: "123",
							},
						},
					},
				}, nil
			},
			getAPIExportsByIdentity: func(identityHash string) ([]*apisv1alpha2.APIExport, error) {
				return []*apisv1alpha2.APIExport{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "fooExport",
							Annotations: map[string]string{
								logicalcluster.AnnotationKey: "someWorkspace",
							},
						},
						Spec: apisv1alpha2.APIExportSpec{
							MaximalPermissionPolicy: &apisv1alpha2.MaximalPermissionPolicy{Local: &apisv1alpha2.LocalAPIExportPolicy{}},
						},
					},
				}, nil
			},
			newDeepSARAuthorizer: func(clusterName logicalcluster.Name) (authorizer.Authorizer, error) {
				return authorizer.AuthorizerFunc(func(ctx context.Context, a authorizer.Attributes) (authorizer.Decision, string, error) {
					return authorizer.DecisionDeny, "access denied", nil
				}), nil
			},
			getFilteredAPIExportEndpointSlice: func(clusterName, filteredAPIExportESName string) (*filteredvwv1alpha1.FilteredAPIExportEndpointSlice, error) {
				return &filteredvwv1alpha1.FilteredAPIExportEndpointSlice{
					ObjectMeta: metav1.ObjectMeta{
						Name: "fooFilteredSlice",
					},
					Spec: filteredvwv1alpha1.FilteredAPIExportEndpointSliceSpec{
						APIExport: filteredvwv1alpha1.ExportBindingReference{
							Name: "fooExport",
						},
					},
				}, nil
			},
			expectedDecision: authorizer.DecisionNoOpinion,
			expectedReason:   `APIExport: "fooExport", workspace: "someWorkspace" RBAC decision: access denied`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := dynamiccontext.WithAPIDomainKey(t.Context(), dynamiccontext.APIDomainKey(tc.apidomainKey))
			auth := &maximalPermissionAuthorizer{
				getFilteredAPIExportEndpointSlice: tc.getFilteredAPIExportEndpointSlice,
				getAPIExport:                      tc.getAPIExport,
				getAPIExportsByIdentity:           tc.getAPIExportsByIdentity,
				newDeepSARAuthorizer:              tc.newDeepSARAuthorizer,
			}
			dec, reason, err := auth.Authorize(ctx, tc.attr)
			errString := ""
			if err != nil {
				errString = err.Error()
			}
			require.Equal(t, errString, tc.expectedErr)
			require.Equal(t, tc.expectedDecision, dec)
			require.Equal(t, tc.expectedReason, reason)
		})
	}
}
