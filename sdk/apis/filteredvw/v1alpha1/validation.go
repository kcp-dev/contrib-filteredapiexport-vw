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
	"reflect"

	"k8s.io/apimachinery/pkg/util/validation/field"
)

// ValidateEndpointSlice validates a FilteredAPIExportEndpointSlice.
func ValidateEndpointSlice(endpointSlice *FilteredAPIExportEndpointSlice) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs, ValidateEndpointSliceReference(endpointSlice.Spec.APIExport, field.NewPath("spec", "apiExport"))...)

	return allErrs
}

// ValidateEndpointSliceReference validates a FilteredAPIExportEndpointSlice's APIExport reference.
func ValidateEndpointSliceReference(reference ExportBindingReference, path *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if reference.Path == "" {
		allErrs = append(allErrs, field.Required(path.Child("export").Child("path"), ""))
	}
	if reference.Name == "" {
		allErrs = append(allErrs, field.Required(path.Child("export").Child("name"), ""))
	}

	return allErrs
}

// ValidateEndpointSliceUpdate validates update for a FilteredAPIExportEndpointSlice.
func ValidateEndpointSliceUpdate(oldSlice, newSlice *FilteredAPIExportEndpointSlice) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs, ValidateEndpointSliceReference(newSlice.Spec.APIExport, field.NewPath("spec", "apiExport"))...)

	if !reflect.DeepEqual(oldSlice.Spec.ObjectSelector, newSlice.Spec.ObjectSelector) {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "objectSelector"), newSlice.Spec.ObjectSelector, "objectSelector is immutable"))
	}

	return allErrs
}
