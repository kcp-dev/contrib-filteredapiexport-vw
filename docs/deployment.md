
# Deployment

## Prerequisites

- A running [kcp](https://github.com/kcp-dev/kcp) instance
- `kubectl` configured to access your kcp instance
- [kcp `kubectl` plugins](https://docs.kcp.io/kcp/main/setup/kubectl-plugin/) installed

## Installing FilteredAPIExportEndpointSlice Resource

The project contains manifests for the `FilteredAPIExportEndpointSlice` resource in the [`deploy`](../deploy) directory. The recommend way to install the resource at the moment is to create the APIResourceSchema and the APIExport in a dedicated workspace (e.g. `provider`) and then bind the API in consumer workspaces that need it.

```bash
# Create the provider workspace
kubectl create workspace provider --enter

# Apply APIResourceSchema and APIExport
kubectl apply -f https://raw.githubusercontent.com/kcp-dev/contrib-filteredapiexport-vw/refs/heads/main/deploy/apiresources/filteredvw.kcp.io/apiresourceschema-filteredapiexportendpointslices.filteredvw.kcp.io.yaml

kubectl apply -f https://raw.githubusercontent.com/kcp-dev/contrib-filteredapiexport-vw/refs/heads/main/deploy/apiresources/filteredvw.kcp.io/apiexport-filteredvw.kcp.io.yaml

# Switch to some workspace, e.g. root
kubectl workspace :root

# Bind the API
kubectl kcp bind apiexport root:provider:filteredvw.kcp.io
```

## Installation

### Option 1: Deploy on Kubernetes

This step assumes the kcp is ran using the following command:

```bash
./hack/run-kcp-in-kind.sh
```

Changes will be required depending on the exact setup.

The first step is to generate kubeconfig files. Set the `kcp-admin.kubeconfig` as your current kubeconfig:

```bash
export KUBECONFIG=kcp-admin.kubeconfig
```

Then switch to the `root` workspace and run the following command to create `kcp.kubeconfig` and set the correct URL (this is an internal URL within the cluster):

```bash
kubectl ws :root

kubectl config view --minify --flatten > kcp.kubeconfig

kubectl --kubeconfig=kcp.kubeconfig config set-cluster --insecure-skip-tls-verify=true "workspace.kcp.io/current" --server "https://kcp:6443/clusters/root"

kubectl --kubeconfig=kcp.kubeconfig config set-credentials kind-kcp --token=system-token
```

After, run a similar command to create `filteredvw.kubeconfig` that'll be used to access the APIExport Virtual Workspace providing FilteredAPIExportEndpointSlice resources. For that we'need `provider` LogicalCluster ID (if your APIExport for FilteredAPIExportEndpointSlice is not in the `provider` workspace, change commands as according):

```bash
kubectl ws :root

provider_cluster_id=$(kubectl get workspace provider -o jsonpath='{.spec.cluster}')

kubectl config view --minify --flatten > filteredvw.kubeconfig

kubectl --kubeconfig=filteredvw.kubeconfig config set-cluster --insecure-skip-tls-verify=true "workspace.kcp.io/current" --server "https://kcp:6443/services/apiexport/${provider_cluster_id}/filteredvw.kcp.io"

kubectl --kubeconfig=filteredvw.kubeconfig config set-credentials kind-kcp --token=system-token
```

Finally, create both kubeconfig files as a Secret in the kind cluster in the `kcp` namespace:

```bash
export KUBECONFIG=kind.kubeconfig

kubectl create secret generic kcp-kubeconfig -n kcp --from-file=kcp.kubeconfig --from-file=filteredvw.kubeconfig
```

Now you can deploy the Virtual Workspace. There's [an example Deployment](./deployment.yaml) provided with the project.

```bash
kubectl create -f deployment.yaml
```

> [!NOTE]
> If you're using this sample Deployment in some other setup, remove `hostAliases` from the manifest.
> The provided `hostAliases` is used to allow controller to connect to kcp in our kind setup where
> `kcp.dev.test` is not a valid domain.

This will also create a Service that exposes the virtual workspace on port 8111. If you want to access the virtual workspace from your local computer, you can use the `port-forward` command like this:

```bash
kubectl port-forward service/kcp-filtered-apiexport-vw 8111:8111
```

### Option 2: Run Locally (Single Shard)

This step assumes the kcp is ran using the following command:

```bash
kcp start -v=4 --batteries-included=admin,workspace-types,user
```

The first step is to prepare the needed kubeconfig files. Take the generated `admin.kubeconfig` file and copy it somewhere else (we'll call it `kcp.kubeconfig` for the purposes of this guide):

```bash
cp .kcp/admin.kubeconfig ./kcp.kubeconfig
```

Open `kcp.kubeconfig` and make the following changes:

- Change `root` context to use the `shard-admin` user
- Ensure `current-context` is set to `root`

Then create one more kubeconfig file based on this one that'll be used to access the APIExport Virtual Workspace providing FilteredAPIExportEndpointSlice resources (we'll call it `filteredvw.kubeconfig` for the purposes of this guide):

```bash
cp ./kcp.kubeconfig ./filteredvw.kubeconfig
```

Open `filteredvw.kubeconfig` and make the following changes:

- Change `base` cluster URL to include `/services/apiexport/<provider-cluster-id>/filteredvw.kcp.io` in the URL
- Ensure `current-context` is set to `base`

The `<provider-cluster-id>` is the LogicalCluster ID of the LogicalCluster that's backing the `provider` workspace. You can get it by describing the `provider` workspace and taking the `Cluster` value.

Finally, build and run the FilteredAPIExport Virtual Workspace. The command will run this Virtual Workspace on port `8111`, but you can change it according to your preference.

```bash
# Build the virtual workspace server
make build

# Run the virtual workspace server
./_build/filtered-apiexport-vw start \
  --secure-port 8111 \
  --shard-name "root" \
  --shard-external-url https://localhost:6443 \
  --tls-cert-file ./.kcp/apiserver.crt \
  --tls-private-key-file ./.kcp/apiserver.key \
  --local-shard-kubeconfig ./kcp.kubeconfig \
  --front-proxy-kubeconfig ./kcp.kubeconfig \
  --authentication-kubeconfig ./kcp.kubeconfig \
  --filtered-apiexport-vw-kubeconfig ./filteredvw.kubeconfig
```

### Option 3: Run Locally (Multi Shard)

This step assumes the kcp is ran using the Make target from the kcp project's root:

```bash
make test-run-sharded-server
```

The first step is to prepare the needed kubeconfig files. Take the generated front-proxy `admin.kubeconfig` file and copy it somewhere else (we'll call it `front-proxy.kubeconfig` for the purposes of this guide):

```bash
cp .kcp/admin.kubeconfig ./front-proxy.kubeconfig
```

Open `front-proxy.kubeconfig` and make the following changes:

- Ensure `current-context` is set to `root`

We also need a local shard kubeconfig. In this example, we'll create a kubeconfig file for the root shard and name it `root.kubeconfig`:

```bash
cp .kcp-0/admin.kubeconfig ./root.kubeconfig
```

Open it and make the following changes:

- Change `root` context to use the `shard-admin` user
- Ensure `current-context` is set to `root`

Finally, we need a kubeconfig file that'll be used to access the APIExport Virtual Workspace providing FilteredAPIExportEndpointSlice resources (we'll call it `filteredvw.kubeconfig` for the purposes of this guide):

```bash
cp .kcp-0/admin.kubeconfig ./filteredvw.kubeconfig
```

Open `filteredvw.kubeconfig` and make the following changes:

- Change `base` cluster URL to include `/services/apiexport/<provider-cluster-id>/filteredvw.kcp.io` in the URL
- Ensure `current-context` is set to `base`

The `<provider-cluster-id>` is the LogicalCluster ID of the LogicalCluster that's backing the `provider` workspace. You can get it by describing the `provider` workspace and taking the `Cluster` value.

Finally, build and run the FilteredAPIExport Virtual Workspace. The command will run this Virtual Workspace on port `8111`, but you can change it according to your preference.

```bash
# Build the virtual workspace server
make build

# Run the virtual workspace server
./_build/filtered-apiexport-vw start \
  --secure-port 8111 \
  --shard-name "root" \
  --shard-external-url https://localhost:6444 \
  --tls-cert-file ./.kcp/apiserver.crt \
  --tls-private-key-file ./.kcp/apiserver.key \
  --client-ca-file ./.kcp/client-ca.crt \
  --local-shard-kubeconfig ./root.kubeconfig \
  --front-proxy-kubeconfig ./front-proxy.kubeconfig \
  --authentication-kubeconfig ./front-proxy.kubeconfig \
  --cache-kubeconfig ./.kcp-cache/cache.kubeconfig \
  --filtered-apiexport-vw-kubeconfig ./filteredvw.kubeconfig
```
