# OnHold

This repository served as an exploration into routing objects between providers using a VirtualWorkspace architecture. Following initial implementation and testing, it was determined that the current design is not viable for real-world production scenarios.

## Core Technical Limitations

The primary challenge lies in the lack of alignment with Kubernetes Resource Model (KRM) semantics during provider transitions. Specifically:

State Discontinuity: When a user modifies a label or routing configuration, the object is immediately removed from the source provider and recreated in the destination provider.

Protocol Incompatibility: These transitions do not natively support standard KRM operations such as WATCH or DELETE. Consequently, controllers or automated systems monitoring these objects lose track of the state during the migration.

Lack of Persistence: To resolve these issues, the system would require the generation of synthetic events and the management of "shadow" objects (to handle finalizers and graceful deletions). This would necessitate a dedicated persistence layer within the workspace, which falls outside the original scope of this lightweight routing experiment.

## Future Exploration

While this project is no longer being actively maintained or updated, the underlying concepts remain open for discussion. If you are interested in evolving these ideas or addressing the persistence requirements mentioned above, please reach out to the maintainers.

# Filtered APIExport Virtual Workspace

[![Go Report Card](https://goreportcard.com/badge/github.com/kcp-dev/contrib-filteredapiexport-vw)](https://goreportcard.com/report/github.com/kcp-dev/contrib-filteredapiexport-vw)
[![GitHub](https://img.shields.io/github/license/kcp-dev/contrib-filteredapiexport-vw)](https://github.com/kcp-dev/contrib-filteredapiexport-vw/blob/main/LICENSE)
[![GitHub release (latest SemVer)](https://img.shields.io/github/v/release/kcp-dev/contrib-filteredapiexport-vw?sort=semver)](https://github.com/kcp-dev/contrib-filteredapiexport-vw/releases/latest)

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

## Deployment

See [the Deployment document](./docs/deployment.md) for more details.

## Quick Start

See [the Quick Start document](./docs/quick-start.md) for more details.

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
