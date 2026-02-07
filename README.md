# Filtered APIExport Virtual Workspace

[![Go Report Card](https://goreportcard.com/badge/github.com/kcp-dev/contrib-filteredapiexport-vw)](https://goreportcard.com/report/github.com/kcp-dev/contrib-filteredapiexport-vw)
[![GitHub](https://img.shields.io/github/license/kcp-dev/contrib-filteredapiexport-vw)](https://github.com/kcp-dev/contrib-filteredapiexport-vw/blob/main/LICENSE)

The Filtered APIExport Virtual Workspace is an external virtual workspace for [kcp](https://github.com/kcp-dev/kcp) that gives Service Providers (APIExport owners) filtered access to their resources in consumer workspaces (bound via APIBindings). This workspace is an extension of the built-in APIExport Virtual Workspace, scoped only to objects that match the provided label selector.

## Overview

When a Service Provider exposes an API via an [APIExport](https://docs.kcp.io/kcp/main/concepts/apis/), they can use the built-in APIExport Virtual Workspace to access all resources across consumer workspaces that have bound the API. The Filtered APIExport Virtual Workspace extends this by allowing:

- **Label-based Filtering**: Access only objects matching a specific label selector through a dedicated virtual workspace URL
- **Multiple Views**: Create different filtered endpoints for different use cases (e.g., by environment, team, or region)
- **Dynamic Scoping**: Objects are filtered at request time based on the configured selector

### How It Works

1. A Service Provider creates a `FilteredAPIExportEndpointSlice` resource that references their APIExport and defines a label selector
2. The controller validates the APIExport reference and generates virtual workspace endpoint URLs in the status
3. The Service Provider uses the endpoint URL to access resources across all consumer workspaces
4. Only objects matching the label selector are visible through this endpoint

## Custom Resource Definition

### FilteredAPIExportEndpointSlice

The `FilteredAPIExportEndpointSlice` resource registers endpoints in the Filtered APIExport Virtual Workspace:

```yaml
apiVersion: filteredvw.kcp.io/v1alpha1
kind: FilteredAPIExportEndpointSlice
metadata:
  name: example
spec:
  export:
    # Path to the workspace containing the APIExport
    path: root:my-org
    # Name of the APIExport
    name: my-apiexport
  objectSelector:
    # Only objects matching this selector will be visible
    matchLabels:
      environment: "prod"
      team: "platform"
```

## Getting Started

### Prerequisites

- A running [kcp](https://github.com/kcp-dev/kcp) instance
- `kubectl` configured to access your kcp instance
- An existing APIExport that you want to provide filtered access to

### Installation

<!-- TODO: Add installation instructions -->

#### Option 1: Deploy to kcp

```bash
# TODO: Add deployment instructions
```

#### Option 2: Run Locally

```bash
# Build the virtual workspace server
make build

# Run the virtual workspace server
./_build/filtered-apiexport-vw \
  --kubeconfig=/path/to/kcp.kubeconfig \
  --secure-serving.bind-address=:6444 \
  --secure-serving.server-cert.cert-file=/path/to/cert.crt \
  --secure-serving.server-cert.key-file=/path/to/cert.key
```

### Quick Start

1. **Create an APIExport** (if you don't have one already):

```yaml
apiVersion: apis.kcp.io/v1alpha2
kind: APIExport
metadata:
  name: my-api
spec:
  # ... your APIExport configuration
```

2. **Create a FilteredAPIExportEndpointSlice**:

```yaml
apiVersion: filteredvw.kcp.io/v1alpha1
kind: FilteredAPIExportEndpointSlice
metadata:
  name: prod-only
spec:
  export:
    path: root:my-org
    name: my-api
  objectSelector:
    matchLabels:
      environment: "prod"
```

3. **Check the status** for generated endpoint URLs:

```bash
kubectl get filteredapiexportendpointslices prod-only -o yaml
```

4. **Access the filtered API** using the endpoint URL from the status:

```bash
kubectl --server=<endpoint-url> get <your-resource>
```

## Development

### Building

```bash
# Build all binaries
make build

# Run code generation
make codegen
```

### Testing

```bash
# Run tests
./hack/run-tests.sh
```

## Troubleshooting

If you encounter problems, please [file an issue](https://github.com/kcp-dev/contrib-filteredapiexport-vw/issues).

## Contributing

Thanks for taking the time to start contributing!

### Before you start

- Please familiarize yourself with the [Code of Conduct](CODE_OF_CONDUCT.md) before contributing.
- See [CONTRIBUTING.md](CONTRIBUTING.md) for instructions on the developer certificate of origin that we require.

### Pull requests

- We welcome pull requests. Feel free to dig through the [issues](https://github.com/kcp-dev/contrib-filteredapiexport-vw/issues) and jump in.

## Changelog

See [the list of releases](https://github.com/kcp-dev/contrib-filteredapiexport-vw/releases) to find out about feature changes.

## License

Apache 2.0
