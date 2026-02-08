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

package v1alpha1

import (
	conditionsv1alpha1 "github.com/kcp-dev/sdk/apis/third_party/conditions/apis/conditions/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +crd
// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,categories=kcp,path=filteredapiexportendpointslices,singular=filteredapiexportendpointslice
// +kubebuilder:printcolumn:name="Export",type="string",JSONPath=".spec.export.name"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// FilteredAPIExportEndpointSlice registers endpoints in the FilteredAPIExport Virtual Workspace to provide access to
// objects of resources provided by the targeted APIExport that match the provided label selector.
// The FilteredAPIExportEndpointSlice status contains the list of endpoints (URLs) that are accessible. Those URLs are
// consumed by the managers to start controllers and informers.
type FilteredAPIExportEndpointSlice struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec holds the desired state:
	// - the targeted APIExport
	// - the label selector to filter objects (only objects matching the selector will be visible via the virtual workspace)
	Spec FilteredAPIExportEndpointSliceSpec `json:"spec,omitempty"`

	// status communicates the observed state, including the list of endpoints (URLs) for this
	// FilteredAPIExportEndpointSlice resource.
	// +optional
	Status FilteredAPIExportEndpointSliceStatus `json:"status,omitempty"`
}

// FilteredAPIExportEndpointSliceSpec defines the desired state of the FilteredAPIExportEndpointSlice.
type FilteredAPIExportEndpointSliceSpec struct {
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="APIExport reference must not be changed"

	// export points to the APIExport.
	APIExport ExportBindingReference `json:"export"`

	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="objectSelector must not be changed"

	// objectSelector is used to filter objects made available via the virtual workspace.
	ObjectSelector FilteredAPIExportObjectSelector `json:"objectSelector,omitempty"`
}

// ExportBindingReference is a reference to an APIExport by cluster and name.
type ExportBindingReference struct {
	// path is a logical cluster path where the APIExport is defined.
	//
	// +required
	// +kubebuilder:validation:Pattern:="^[a-z0-9]([-a-z0-9]*[a-z0-9])?(:[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$"
	Path string `json:"path,omitempty"`

	// name is the name of the APIExport that describes the API.
	//
	// +required
	// +kubebuilder:validation:Required
	// +kube:validation:MinLength=1
	Name string `json:"name"`
}

// FilteredAPIExportObjectSelector is a label selector for objects made available via the virtual workspace.
type FilteredAPIExportObjectSelector struct {
	metav1.LabelSelector `json:",inline"`
}

// FilteredAPIExportEndpointSliceStatus defines the observed state of FilteredAPIExportEndpointSlice.
type FilteredAPIExportEndpointSliceStatus struct {
	// +optional

	// conditions is a list of conditions that apply to the FilteredAPIExportEndpointSlice.
	Conditions conditionsv1alpha1.Conditions `json:"conditions,omitempty"`

	// // endpoints contains all the URLs of the APIExport service.
	// //
	// // +optional
	// // +listType=map
	// // +listMapKey=url
	// FilteredAPIExportEndpoints []FilteredAPIExportEndpoint `json:"endpoints"`
}

const (
	// APIExportValid is a condition for FilteredAPIExportEndpointSlice that reflects the validity of the referenced APIExport.
	APIExportValid conditionsv1alpha1.ConditionType = "APIExportValid"
	// APIExportNotFoundReason is a reason for the APIExportValid condition that the referenced APIExport is not found.
	APIExportNotFoundReason = "APIExportNotFound"
	// InternalErrorReason is a reason used by multiple conditions that something went wrong.
	InternalErrorReason = "InternalError"
)

// Using a struct provides an extension point

// APIExportEndpoint contains the endpoint information of an APIExport service for a specific shard.
// type FilteredAPIExportEndpoint struct {
//
// 	// +kubebuilder:validation:MinLength=1
// 	// +kubebuilder:format:URL
// 	// +required
//
// 	// url is a FilteredAPIExport virtual workspace URL for this instance of FilteredAPIExportEndpoint.
// 	URL string `json:"url"`
// }

func (in *FilteredAPIExportEndpointSlice) GetConditions() conditionsv1alpha1.Conditions {
	return in.Status.Conditions
}

func (in *FilteredAPIExportEndpointSlice) SetConditions(conditions conditionsv1alpha1.Conditions) {
	in.Status.Conditions = conditions
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// FilteredAPIExportEndpointSliceList is a list of FilteredAPIExportEndpointSlice resources.
type FilteredAPIExportEndpointSliceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []FilteredAPIExportEndpointSlice `json:"items"`
}
