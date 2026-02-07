#!/usr/bin/env bash

# Copyright 2025 The KCP Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -euo pipefail

cd $(dirname $0)/..
source hack/lib.sh

BOILERPLATE_HEADER="$(realpath hack/boilerplate/generated/boilerplate.go.txt)"
SDK_MODULE="github.com/kcp-dev/contrib-filteredapiexport-vw/sdk"
APIS_PKG="${SDK_MODULE}/apis"
CLIENT_PKG="${SDK_MODULE}/client"

CONTROLLER_GEN="$(UGET_PRINT_PATH=absolute make --no-print-directory install-controller-gen)"
APPLYCONFIGURATION_GEN="$(UGET_PRINT_PATH=absolute make --no-print-directory install-applyconfiguration-gen)"
CLIENT_GEN="$(UGET_PRINT_PATH=absolute make --no-print-directory install-client-gen)"
KCP_CODEGEN="$(UGET_PRINT_PATH=absolute make --no-print-directory install-kcp-codegen)"
GOIMPORTS="$(UGET_PRINT_PATH=absolute make --no-print-directory install-goimports)"

set -x

cd sdk
rm -rf -- client

"$CONTROLLER_GEN" \
  "object:headerFile=${BOILERPLATE_HEADER}" \
  paths=./apis/...

"$APPLYCONFIGURATION_GEN" \
  --go-header-file "${BOILERPLATE_HEADER}" \
  --output-pkg "${CLIENT_PKG}/applyconfiguration" \
  --output-dir "client/applyconfiguration" \
  "${APIS_PKG}/filteredvw/v1alpha1"

"$CLIENT_GEN" \
  --go-header-file "${BOILERPLATE_HEADER}" \
  --output-pkg "${CLIENT_PKG}/clientset" \
  --output-dir "client/clientset" \
  --input "${APIS_PKG}/filteredvw/v1alpha1" \
  --input-base "" \
  --apply-configuration-package=github.com/kcp-dev/contrib-filteredapiexport-vw/sdk/client/applyconfiguration \
  --clientset-name "versioned"

"$KCP_CODEGEN" \
  "client:headerFile=${BOILERPLATE_HEADER},apiPackagePath=${APIS_PKG},outputPackagePath=${CLIENT_PKG},singleClusterClientPackagePath=${CLIENT_PKG}/clientset/versioned,singleClusterApplyConfigurationsPackagePath=${CLIENT_PKG}/applyconfiguration" \
  "informer:headerFile=${BOILERPLATE_HEADER},apiPackagePath=${APIS_PKG},outputPackagePath=${CLIENT_PKG},singleClusterClientPackagePath=${CLIENT_PKG}/clientset/versioned" \
  "lister:headerFile=${BOILERPLATE_HEADER},apiPackagePath=${APIS_PKG}" \
  "paths=./apis/..." \
  "output:dir=./client"

# Use openshift's import fixer because gimps fails to parse some of the files;
# its output is identical to how gimps would sort the imports, but it also fixes
# the misplaced go:build directives.
"$GOIMPORTS" .
