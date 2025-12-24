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
	"fmt"
	"strings"

	"github.com/kcp-dev/logicalcluster/v3"
	apisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	apisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	apisv1alpha2informers "github.com/kcp-dev/sdk/client/informers/externalversions/apis/v1alpha2"

	filteredapiexportv1alpha1 "github.com/kcp-dev/contrib-filteredapiexport-vw/sdk/apis/filteredapiexport/v1alpha1"
	filteredapiexportinformers "github.com/kcp-dev/contrib-filteredapiexport-vw/sdk/informers/externalversions/filteredapiexport/v1alpha1"

	"github.com/kcp-dev/kcp/pkg/authorization/delegated"
	"github.com/kcp-dev/kcp/pkg/indexers"
	dynamiccontext "github.com/kcp-dev/kcp/pkg/virtual/framework/dynamic/context"

	kcpkubernetesclientset "github.com/kcp-dev/client-go/kubernetes"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/authorization/authorizer"
)

type maximalPermissionAuthorizer struct {
	getFilteredAPIExportEndpointSlice func(clusterName, filteredAPIExportESName string) (*filteredapiexportv1alpha1.FilteredAPIExportEndpointSlice, error)
	getAPIExport                      func(clusterName, apiExportName string) (*apisv1alpha2.APIExport, error)
	newDeepSARAuthorizer              func(clusterName logicalcluster.Name) (authorizer.Authorizer, error)
	getAPIExportsByIdentity           func(identityHash string) ([]*apisv1alpha2.APIExport, error)
}

// NewMaximalPermissionAuthorizer creates an authorizer that checks the maximal permission policy
// for the requested resource if the resource is a claimed resource in the requested API export.
// The check is omitted if the requested resource itself is not associated with an API export.
//
// If the request is a cluster request the authorizer skips authorization if the request is not for a bound resource.
// If the request is a wildcard request this check is skipped because no unique API binding can be determined.
func NewMaximalPermissionAuthorizer(deepSARClient kcpkubernetesclientset.ClusterInterface, filteredAPIExportESInformer filteredapiexportinformers.FilteredAPIExportEndpointSliceClusterInformer, apiExportInformer apisv1alpha2informers.APIExportClusterInformer) authorizer.Authorizer {
	filteredAPIExportESLister := filteredAPIExportESInformer.Lister()
	apiExportLister := apiExportInformer.Lister()
	apiExportIndexer := apiExportInformer.Informer().GetIndexer()

	return &maximalPermissionAuthorizer{
		getFilteredAPIExportEndpointSlice: func(clusterName, filteredAPIExportESName string) (*filteredapiexportv1alpha1.FilteredAPIExportEndpointSlice, error) {
			return filteredAPIExportESLister.Cluster(logicalcluster.Name(clusterName)).Get(filteredAPIExportESName)
		},
		getAPIExport: func(clusterName, apiExportName string) (*apisv1alpha2.APIExport, error) {
			return apiExportLister.Cluster(logicalcluster.Name(clusterName)).Get(apiExportName)
		},
		getAPIExportsByIdentity: func(identityHash string) ([]*apisv1alpha2.APIExport, error) {
			return indexers.ByIndex[*apisv1alpha2.APIExport](apiExportIndexer, indexers.APIExportByIdentity, identityHash)
		},
		newDeepSARAuthorizer: func(clusterName logicalcluster.Name) (authorizer.Authorizer, error) {
			return delegated.NewDelegatedAuthorizer(clusterName, deepSARClient, delegated.Options{})
		},
	}
}

func (a *maximalPermissionAuthorizer) Authorize(ctx context.Context, attr authorizer.Attributes) (authorizer.Decision, string, error) {
	apiDomainKey := dynamiccontext.APIDomainKeyFrom(ctx)
	parts := strings.Split(string(apiDomainKey), "/")
	if len(parts) < 2 {
		return authorizer.DecisionNoOpinion, "", fmt.Errorf("invalid API domain key")
	}

	filteredAPIExportESCluster := parts[0]
	filteredAPIExportESName := parts[1]

	filteredAPIExportES, err := a.getFilteredAPIExportEndpointSlice(filteredAPIExportESCluster, filteredAPIExportESName)
	if apierrors.IsNotFound(err) {
		return authorizer.DecisionNoOpinion, "", fmt.Errorf("FilteredAPIExportEndpointSlice not found: %w", err)
	}
	if err != nil {
		return authorizer.DecisionNoOpinion, "", err
	}

	claimingAPIExportCluster := filteredAPIExportES.Spec.APIExport.Path
	claimingAPIExportName := filteredAPIExportES.Spec.APIExport.Name

	claimingAPIExport, err := a.getAPIExport(claimingAPIExportCluster, claimingAPIExportName)
	if apierrors.IsNotFound(err) {
		return authorizer.DecisionNoOpinion, "", fmt.Errorf("APIExport not found: %w", err)
	}
	if err != nil {
		return authorizer.DecisionNoOpinion, "", err
	}

	claimedIdentityHash, found := getClaimedIdentity(claimingAPIExport, attr)
	if !found {
		// it's a resource in the claiming API export, hence unclaimed
		return authorizer.DecisionAllow, fmt.Sprintf("unclaimed resource in APIExport: %q, workspace :%q",
			claimingAPIExport.Name, logicalcluster.From(claimingAPIExport)), nil
	}
	if claimedIdentityHash == "" {
		// it's a native k8s resource (secret, configmap, ...), or a system kcp CRD resource (apis.kcp.io)
		// For neither case a maximum permission policy can exist.
		return authorizer.DecisionAllow, fmt.Sprintf("unclaimable resource, identity hash not set in claiming APIExport: %q, workspace :%q",
			claimingAPIExport.Name, logicalcluster.From(claimingAPIExport)), nil
	}

	apiExportsProvidingClaimedResources, err := a.getAPIExportsByIdentity(claimedIdentityHash)
	if err != nil {
		return authorizer.DecisionNoOpinion, "", fmt.Errorf("error getting APIExport identity: %q: %w", claimedIdentityHash, err)
	}

	if len(apiExportsProvidingClaimedResources) == 0 {
		// a claimed identity hash exists but not APIExport can be found referring to it (potentially eventually consistent).
		// In this case be safe and deny the request, forcing the caller to retry.
		return authorizer.DecisionDeny, fmt.Sprintf("no APIExport providing claimed resources found for identity hash: %q", claimedIdentityHash), nil
	}

	// multiple claimed APIExports can share the same identity hash (even in different workspaces).
	// All maximum permission policies must grant access because at this point no deterministic APIExport can be picked.
	for _, apiExportProvidingClaimedResource := range apiExportsProvidingClaimedResources {
		if apiExportProvidingClaimedResource.Spec.MaximalPermissionPolicy == nil {
			continue
		}

		if apiExportProvidingClaimedResource.Spec.MaximalPermissionPolicy.Local == nil {
			continue
		}

		authz, err := a.newDeepSARAuthorizer(logicalcluster.From(apiExportProvidingClaimedResource))
		if err != nil {
			return authorizer.DecisionNoOpinion, "", fmt.Errorf("error executing deep SAR in APIExport name: %q, workspace: %q: %w",
				apiExportProvidingClaimedResource.Name, logicalcluster.From(apiExportProvidingClaimedResource), err)
		}

		dec, reason, err := authz.Authorize(ctx, prefixAttributes(attr))
		if err != nil {
			return authorizer.DecisionNoOpinion, "", fmt.Errorf("error authorizing against APIExport name: %q, workspace: %q: %w",
				apiExportProvidingClaimedResource.Name, logicalcluster.From(apiExportProvidingClaimedResource), err)
		}

		// all maximum permission policies must grant access
		if dec != authorizer.DecisionAllow {
			return authorizer.DecisionNoOpinion, fmt.Sprintf("APIExport: %q, workspace: %q RBAC decision: %v",
				apiExportProvidingClaimedResource.Name, logicalcluster.From(apiExportProvidingClaimedResource), reason), nil
		}
	}

	return authorizer.DecisionAllow, "all claimed APIExports granted access", nil
}

func getClaimedIdentity(apiExport *apisv1alpha2.APIExport, attr authorizer.Attributes) (string, bool) {
	for i := range apiExport.Spec.PermissionClaims {
		if apiExport.Spec.PermissionClaims[i].Resource == attr.GetResource() &&
			apiExport.Spec.PermissionClaims[i].Group == attr.GetAPIGroup() {
			return apiExport.Spec.PermissionClaims[i].IdentityHash, true
		}
	}
	return "", false
}

func prefixAttributes(attr authorizer.Attributes) *authorizer.AttributesRecord {
	prefixedUser := &user.DefaultInfo{
		Name:  apisv1alpha1.MaximalPermissionPolicyRBACUserGroupPrefix + attr.GetUser().GetName(),
		UID:   attr.GetUser().GetUID(),
		Extra: attr.GetUser().GetExtra(),
	}

	prefixedUser.Groups = make([]string, 0, len(attr.GetUser().GetGroups()))
	for _, g := range attr.GetUser().GetGroups() {
		prefixedUser.Groups = append(prefixedUser.Groups, apisv1alpha1.MaximalPermissionPolicyRBACUserGroupPrefix+g)
	}

	return &authorizer.AttributesRecord{
		User:            prefixedUser,
		Verb:            attr.GetVerb(),
		Namespace:       attr.GetNamespace(),
		APIGroup:        attr.GetAPIGroup(),
		APIVersion:      attr.GetAPIVersion(),
		Resource:        attr.GetResource(),
		Subresource:     attr.GetSubresource(),
		Name:            attr.GetName(),
		ResourceRequest: attr.IsResourceRequest(),
		Path:            attr.GetPath(),
	}
}
