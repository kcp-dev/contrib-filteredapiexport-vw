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

package indexers

import (
	"fmt"

	"github.com/kcp-dev/logicalcluster/v3"

	filteredapiexportv1alpha1 "github.com/kcp-dev/contrib-filteredapiexport-vw/sdk/apis/filteredapiexport/v1alpha1"
)

// FilteredAPIExportEndpointSliceByAPIExport is the indexer name for retrieving FilteredAPIExportEndpointSlices by their APIExport's Reference Path and Name.
const FilteredAPIExportEndpointSliceByAPIExport = "FilteredAPIExportEndpointSliceByAPIExport"

// IndexFilteredAPIExportEndpointSliceByAPIExportFunc indexes the FilteredAPIExportEndpointSlice by their APIExport's Reference Path and Name.
func IndexFilteredAPIExportEndpointSliceByAPIExport(obj interface{}) ([]string, error) {
	filteredAPIExportES, ok := obj.(*filteredapiexportv1alpha1.FilteredAPIExportEndpointSlice)
	if !ok {
		return []string{}, fmt.Errorf("obj %T is not a FilteredAPIExportEndpointSlice", obj)
	}

	var result []string
	pathRemote := logicalcluster.NewPath(filteredAPIExportES.Spec.APIExport.Path)
	if !pathRemote.Empty() {
		result = append(result, pathRemote.Join(filteredAPIExportES.Spec.APIExport.Name).String())
	}
	pathLocal := logicalcluster.From(filteredAPIExportES).Path()
	if !pathLocal.Empty() {
		result = append(result, pathLocal.Join(filteredAPIExportES.Spec.APIExport.Name).String())
	}

	return result, nil
}
