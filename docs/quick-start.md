# Quick Start

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

3. **Create the kubeconfig file** to access the FilteredAPIExport Virtual Workspace for the createdFilteredAPIExportEndpointSlice.

The easiest way to do this is to modify the `filteredvw.kubeconfig` kubeconfig file from the "Getting Started" step.

Copy it and then open it in a text editor:

```bash
cp ./filteredvw.kubeconfig ./prod-only.kubeconfig
```

Make the following changes:

- Change `base` cluster URL to `/services/filtered-apiexport/<cluster-id>/<filteredapiexportendpointslice-name>/clusters/<consumer-cluster-id>` in the URL

The `<cluster-id>` is the LogicalCluster ID of the LogicalCluster where the FilteredAPIExportEndpointSlice is created. The `<consumer-cluster-id>` is the LogicalClusterID of the consumer LogicalCluster, i.e. of the LogicalCluster containing objects provided by that APIExport. `<consumer-cluster-id>` can also be `*` to get resources from all LogicalClusters/workspaces on that shard (read-only operations supported only). You can get the LogicalCluster ID by describing the workspace and taking the `Cluster` value.

`<filteredapiexportendpointslice-name>` is the name of FilteredAPIExportEndpointSlice, in this example `prod-only`.

4. **Set the kubeconfig file as active**:

```bash
export KUBECONFIG=./prod-only.kubeconfig
```

5. **Get your resources** using `kubectl`:

```bash
kubectl get <resource-name>
```

This will return you objects of the given resoruce only that are matching the label selector provided in the FilteredAPIExportEndpointSlice object. Any new object that is created will have those labels, unless there are conflicts, in which case the appropriate error will be returned.
