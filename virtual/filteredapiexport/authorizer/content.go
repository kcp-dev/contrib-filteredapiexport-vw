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
	apisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"

	"github.com/kcp-dev/kcp/pkg/authorization/delegated"
	dynamiccontext "github.com/kcp-dev/kcp/pkg/virtual/framework/dynamic/context"

	kcpkubernetesclientset "github.com/kcp-dev/client-go/kubernetes"
	"k8s.io/apiserver/pkg/authorization/authorizer"
)

type filteredAPIExportsContentAuthorizer struct {
	newDelegatedAuthorizer func(clusterName string) (authorizer.Authorizer, error)
	delegate               authorizer.Authorizer
}

// NewFilteredAPIExportsContentAuthorizer creates a new authorizer that checks
// if the user has access to the `filteredapiexportendpointslice/content` subresource using the same verb as the requested resource.
// The given kube cluster client is used to execute a SAR request against the cluster of the current in-flight API export.
// If the SAR decision allows access, the given delegate authorizer is executed to proceed the authorizer chain,
// else access is denied.
func NewFilteredAPIExportsContentAuthorizer(delegate authorizer.Authorizer, kubeClusterClient kcpkubernetesclientset.ClusterInterface) authorizer.Authorizer {
	return &filteredAPIExportsContentAuthorizer{
		newDelegatedAuthorizer: func(clusterName string) (authorizer.Authorizer, error) {
			return delegated.NewDelegatedAuthorizer(logicalcluster.Name(clusterName), kubeClusterClient, delegated.Options{})
		},
		delegate: delegate,
	}
}

func (a *filteredAPIExportsContentAuthorizer) Authorize(ctx context.Context, attr authorizer.Attributes) (authorizer.Decision, string, error) {
	apiDomainKey := dynamiccontext.APIDomainKeyFrom(ctx)
	parts := strings.Split(string(apiDomainKey), "/")
	if len(parts) < 2 {
		return authorizer.DecisionNoOpinion, "", fmt.Errorf("invalid API domain key")
	}

	filteredAPIExportESCluster, filteredAPIExportESName := parts[0], parts[1]
	authz, err := a.newDelegatedAuthorizer(filteredAPIExportESCluster)
	if err != nil {
		return authorizer.DecisionNoOpinion, "",
			fmt.Errorf("error creating delegated authorizer for FilteredAPIExportEndpointSlice %q, workspace %q: %w", filteredAPIExportESName, filteredAPIExportESCluster, err)
	}

	SARAttributes := authorizer.AttributesRecord{
		APIGroup:        apisv1alpha2.SchemeGroupVersion.Group,
		APIVersion:      apisv1alpha2.SchemeGroupVersion.Version,
		User:            attr.GetUser(),
		Verb:            attr.GetVerb(),
		Name:            filteredAPIExportESName,
		Resource:        "filteredapiexportendpointslices",
		ResourceRequest: true,
		Subresource:     "content",
	}

	dec, reason, err := authz.Authorize(ctx, SARAttributes)
	if err != nil {
		return authorizer.DecisionNoOpinion, "",
			fmt.Errorf("error authorizing RBAC in FilteredAPIExportEndpointSlice %q, workspace %q: %w", filteredAPIExportESName, filteredAPIExportESCluster, err)
	}

	if dec == authorizer.DecisionAllow {
		return a.delegate.Authorize(ctx, attr)
	}

	return dec, fmt.Sprintf("FilteredAPIExportEndpointSlice: %q, workspace: %q RBAC decision: %v",
		filteredAPIExportESName, filteredAPIExportESCluster, reason), nil
}
