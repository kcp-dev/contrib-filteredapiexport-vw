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

package endpointsliceurls

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	kcpcache "github.com/kcp-dev/apimachinery/v2/pkg/cache"
	"github.com/kcp-dev/logicalcluster/v3"
	apisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	"github.com/kcp-dev/sdk/apis/core"
	corev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	apisv1alpha2informers "github.com/kcp-dev/sdk/client/informers/externalversions/apis/v1alpha2"
	corev1alpha1informers "github.com/kcp-dev/sdk/client/informers/externalversions/core/v1alpha1"

	filteredapiexportindexers "github.com/kcp-dev/contrib-filteredapiexport-vw/internal/indexers"
	filteredvwv1alpha1 "github.com/kcp-dev/contrib-filteredapiexport-vw/sdk/apis/filteredvw/v1alpha1"
	filteredvwv1alpha1apply "github.com/kcp-dev/contrib-filteredapiexport-vw/sdk/client/applyconfiguration/filteredvw/v1alpha1"
	filteredvwclientset "github.com/kcp-dev/contrib-filteredapiexport-vw/sdk/client/clientset/versioned/cluster"
	filteredvwinformers "github.com/kcp-dev/contrib-filteredapiexport-vw/sdk/client/informers/externalversions/filteredvw/v1alpha1"

	"github.com/kcp-dev/kcp/pkg/indexers"
	"github.com/kcp-dev/kcp/pkg/logging"
	"github.com/kcp-dev/kcp/pkg/tombstone"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

const (
	ControllerName = "kcp-filteredapiexport-endpointslice-urls"
)

// NewController returns a new controller for FilteredAPIExportEndpointSlices.
// Shards and APIExports are read from the cache server.
func NewController(
	shardName string,
	globalAPIExportClusterInformer apisv1alpha2informers.APIExportClusterInformer,
	globalShardClusterInformer corev1alpha1informers.ShardClusterInformer,
	filteredAPIExportEndpointSliceClusterInformer filteredvwinformers.FilteredAPIExportEndpointSliceClusterInformer,
	apiBindingInformer apisv1alpha2informers.APIBindingClusterInformer,
	filteredAPIExportClient filteredvwclientset.ClusterInterface,
) (*controller, error) {
	c := &controller{
		shardName: shardName,
		queue: workqueue.NewTypedRateLimitingQueueWithConfig(
			workqueue.DefaultTypedControllerRateLimiter[string](),
			workqueue.TypedRateLimitingQueueConfig[string]{
				Name: ControllerName,
			},
		),
		getMyShard: func() (*corev1alpha1.Shard, error) {
			return globalShardClusterInformer.Cluster(core.RootCluster).Lister().Get(shardName)
		},
		listShards: func() ([]*corev1alpha1.Shard, error) {
			return globalShardClusterInformer.Cluster(core.RootCluster).Lister().List(labels.Everything())
		},
		getFilteredAPIExportEndpointSlice: func(path logicalcluster.Path, name string) (*filteredvwv1alpha1.FilteredAPIExportEndpointSlice, error) {
			return indexers.ByPathAndName[*filteredvwv1alpha1.FilteredAPIExportEndpointSlice](filteredvwv1alpha1.Resource("filteredapiexportendpointslices"), filteredAPIExportEndpointSliceClusterInformer.Informer().GetIndexer(), path, name)
		},
		getAPIExport: func(path logicalcluster.Path, name string) (*apisv1alpha2.APIExport, error) {
			return indexers.ByPathAndName[*apisv1alpha2.APIExport](apisv1alpha2.Resource("apiexports"), globalAPIExportClusterInformer.Informer().GetIndexer(), path, name)
		},
		listAPIBindingsByAPIExport: func(export *apisv1alpha2.APIExport) ([]*apisv1alpha2.APIBinding, error) {
			// binding keys by full path
			keys := sets.New[string]()
			if path := logicalcluster.NewPath(export.Annotations[core.LogicalClusterPathAnnotationKey]); !path.Empty() {
				pathKeys, err := apiBindingInformer.Informer().GetIndexer().IndexKeys(indexers.APIBindingsByAPIExport, path.Join(export.Name).String())
				if err != nil {
					return nil, err
				}
				keys.Insert(pathKeys...)
			}

			clusterKeys, err := apiBindingInformer.Informer().GetIndexer().IndexKeys(indexers.APIBindingsByAPIExport, logicalcluster.From(export).Path().Join(export.Name).String())
			if err != nil {
				return nil, err
			}
			keys.Insert(clusterKeys...)

			bindings := make([]*apisv1alpha2.APIBinding, 0, keys.Len())
			for _, key := range sets.List[string](keys) {
				binding, exists, err := apiBindingInformer.Informer().GetIndexer().GetByKey(key)
				if err != nil {
					utilruntime.HandleError(err)
					continue
				} else if !exists {
					utilruntime.HandleError(fmt.Errorf("APIBinding %q does not exist", key))
					continue
				}
				bindings = append(bindings, binding.(*apisv1alpha2.APIBinding))
			}
			return bindings, nil
		},
		patchFilteredAPIExportEndpointSlice: func(ctx context.Context, cluster logicalcluster.Path, patch *filteredvwv1alpha1apply.FilteredAPIExportEndpointSliceApplyConfiguration) error {
			_, err := filteredAPIExportClient.FilteredvwV1alpha1().FilteredAPIExportEndpointSlices().Cluster(cluster).ApplyStatus(ctx, patch, metav1.ApplyOptions{
				FieldManager: shardName,
			})
			return err
		},
		filteredAPIExportEndpointSliceClusterInformer: filteredAPIExportEndpointSliceClusterInformer,
	}

	logger := logging.WithReconciler(klog.Background(), ControllerName)

	_, _ = filteredAPIExportEndpointSliceClusterInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			c.enqueueFilteredAPIExportEndpointSlice(tombstone.Obj[*filteredvwv1alpha1.FilteredAPIExportEndpointSlice](obj), logger, "")
		},
		UpdateFunc: func(_, newObj interface{}) {
			c.enqueueFilteredAPIExportEndpointSlice(tombstone.Obj[*filteredvwv1alpha1.FilteredAPIExportEndpointSlice](newObj), logger, "")
		},
		DeleteFunc: func(obj interface{}) {
			c.enqueueFilteredAPIExportEndpointSlice(tombstone.Obj[*filteredvwv1alpha1.FilteredAPIExportEndpointSlice](obj), logger, "")
		},
	})

	_, _ = apiBindingInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			c.enqueueFilteredAPIExportEndpointSliceByAPIBinding(tombstone.Obj[*apisv1alpha2.APIBinding](obj), logger)
		},
		UpdateFunc: func(_, newObj interface{}) {
			c.enqueueFilteredAPIExportEndpointSliceByAPIBinding(tombstone.Obj[*apisv1alpha2.APIBinding](newObj), logger)
		},
		DeleteFunc: func(obj interface{}) {
			c.enqueueFilteredAPIExportEndpointSliceByAPIBinding(tombstone.Obj[*apisv1alpha2.APIBinding](obj), logger)
		},
	})

	// Touch the Shards informer so it gets registered with the factory and started.
	// We don't need to react to Shard changes, but we need the lister to be populated
	// for reconciliation.
	_ = globalShardClusterInformer.Informer()

	// TODO: revisit this in the future once we are able to cache non-default resources
	// _, _ = globalFilteredAPIExportEndpointSliceClusterInformer.Informer().AddEventHandler(events.WithoutSyncs(cache.ResourceEventHandlerFuncs{
	// 	AddFunc: func(obj interface{}) {
	// 		c.enqueueFilteredAPIExportEndpointSlice(tombstone.Obj[*filteredvwv1alpha1.FilteredAPIExportEndpointSlice](obj), logger, " from cache")
	// 	},
	// 	UpdateFunc: func(_, newObj interface{}) {
	// 		c.enqueueFilteredAPIExportEndpointSlice(tombstone.Obj[*filteredvwv1alpha1.FilteredAPIExportEndpointSlice](newObj), logger, " from cache")
	// 	},
	// 	DeleteFunc: func(obj interface{}) {
	// 		c.enqueueFilteredAPIExportEndpointSlice(tombstone.Obj[*filteredvwv1alpha1.FilteredAPIExportEndpointSlice](obj), logger, " from cache")
	// 	},
	// }))

	return c, nil
}

// controller reconciles FilteredAPIExportEndpointSlices. It ensures that the shard endpoints are populated
// in the status of every APIExportEndpointSlices.
type controller struct {
	queue     workqueue.TypedRateLimitingInterface[string]
	shardName string

	getMyShard                          func() (*corev1alpha1.Shard, error)
	getFilteredAPIExportEndpointSlice   func(path logicalcluster.Path, name string) (*filteredvwv1alpha1.FilteredAPIExportEndpointSlice, error)
	getAPIExport                        func(path logicalcluster.Path, name string) (*apisv1alpha2.APIExport, error)
	listShards                          func() ([]*corev1alpha1.Shard, error)
	listAPIBindingsByAPIExport          func(apiexport *apisv1alpha2.APIExport) ([]*apisv1alpha2.APIBinding, error)
	patchFilteredAPIExportEndpointSlice func(ctx context.Context, cluster logicalcluster.Path, patch *filteredvwv1alpha1apply.FilteredAPIExportEndpointSliceApplyConfiguration) error

	filteredAPIExportEndpointSliceClusterInformer filteredvwinformers.FilteredAPIExportEndpointSliceClusterInformer
}

func (c *controller) enqueueFilteredAPIExportEndpointSliceByAPIBinding(binding *apisv1alpha2.APIBinding, logger logr.Logger) {
	{ // local to shard
		keys := sets.New[string]()
		if path := logicalcluster.NewPath(binding.Spec.Reference.Export.Path); !path.Empty() { // This is remote apibinding.
			pathKeys, err := c.filteredAPIExportEndpointSliceClusterInformer.Informer().GetIndexer().IndexKeys(filteredapiexportindexers.FilteredAPIExportEndpointSliceByAPIExport, path.Join(binding.Spec.Reference.Export.Name).String())
			if err != nil {
				utilruntime.HandleError(err)
				return
			}
			keys.Insert(pathKeys...)
		} else {
			// This is local apibinding to the export. Meaning it has path set to empty string, so apiexport is in the same cluster as the binding.
			// While our CLI does not allow this, it is possible to create such a binding via the API.
			clusterKeys, err := c.filteredAPIExportEndpointSliceClusterInformer.Informer().GetIndexer().IndexKeys(filteredapiexportindexers.FilteredAPIExportEndpointSliceByAPIExport, logicalcluster.From(binding).Path().Join(binding.Spec.Reference.Export.Name).String())
			if err != nil {
				utilruntime.HandleError(err)
				return
			}
			keys.Insert(clusterKeys...)
		}

		for _, key := range sets.List[string](keys) {
			slice, exists, err := c.filteredAPIExportEndpointSliceClusterInformer.Informer().GetIndexer().GetByKey(key)
			if err != nil {
				utilruntime.HandleError(err)
				continue
			} else if !exists {
				continue
			}
			c.enqueueFilteredAPIExportEndpointSlice(tombstone.Obj[*filteredvwv1alpha1.FilteredAPIExportEndpointSlice](slice), logger, " because of APIBinding")
		}
	}
}

// enqueueFilteredAPIExportEndpointSlice enqueues a FilteredAPIExportEndpointSlice.
func (c *controller) enqueueFilteredAPIExportEndpointSlice(obj *filteredvwv1alpha1.FilteredAPIExportEndpointSlice, logger logr.Logger, logSuffix string) {
	key, err := kcpcache.DeletionHandlingMetaClusterNamespaceKeyFunc(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}

	logger.V(4).Info(fmt.Sprintf("queueing FilteredAPIExportEndpointSlice%s", logSuffix))
	c.queue.Add(key)
}

// Start starts the controller, which stops when ctx.Done() is closed.
func (c *controller) Start(ctx context.Context, numThreads int) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	logger := logging.WithReconciler(klog.FromContext(ctx), ControllerName)
	ctx = klog.NewContext(ctx, logger)
	logger.Info("Starting controller")
	defer logger.Info("Shutting down controller")

	for range numThreads {
		go wait.UntilWithContext(ctx, c.startWorker, time.Second)
	}

	<-ctx.Done()
}

func (c *controller) startWorker(ctx context.Context) {
	for c.processNextWorkItem(ctx) {
	}
}

func (c *controller) processNextWorkItem(ctx context.Context) bool {
	// Wait until there is a new item in the working queue
	k, quit := c.queue.Get()
	if quit {
		return false
	}
	key := k

	logger := logging.WithQueueKey(klog.FromContext(ctx), key)
	ctx = klog.NewContext(ctx, logger)
	logger.V(4).Info("processing key")

	// No matter what, tell the queue we're done with this key, to unblock
	// other workers.
	defer c.queue.Done(key)

	if requeue, err := c.process(ctx, key); err != nil {
		utilruntime.HandleError(fmt.Errorf("%q controller failed to sync %q, err: %w", ControllerName, key, err))
		c.queue.AddRateLimited(key)
		return true
	} else if requeue {
		// only requeue if we didn't error, but we still want to requeue
		c.queue.Add(key)
		return true
	}
	c.queue.Forget(key)
	return true
}

func (c *controller) process(ctx context.Context, key string) (bool, error) {
	clusterName, _, name, err := kcpcache.SplitMetaClusterNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(err)
		return false, nil
	}
	obj, err := c.getFilteredAPIExportEndpointSlice(clusterName.Path(), name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil // object deleted before we handled it
		}
		return false, err
	}

	obj = obj.DeepCopy()

	logger := logging.WithObject(klog.FromContext(ctx), obj)
	ctx = klog.NewContext(ctx, logger)

	var errs []error
	requeue, err := c.reconcile(ctx, obj)
	if err != nil {
		errs = append(errs, err)
	}

	return requeue, utilerrors.NewAggregate(errs)
}

// InstallIndexers adds the additional indexers that this controller requires to the informers.
func InstallIndexers(
	filteredAPIExportEndpointSliceClusterInformer filteredvwinformers.FilteredAPIExportEndpointSliceClusterInformer,
	apiBindingInformer apisv1alpha2informers.APIBindingClusterInformer,
) {
	indexers.AddIfNotPresentOrDie(filteredAPIExportEndpointSliceClusterInformer.Informer().GetIndexer(), cache.Indexers{
		indexers.ByLogicalClusterPathAndName: indexers.IndexByLogicalClusterPathAndName,
	})
	indexers.AddIfNotPresentOrDie(filteredAPIExportEndpointSliceClusterInformer.Informer().GetIndexer(), cache.Indexers{
		filteredapiexportindexers.FilteredAPIExportEndpointSliceByAPIExport: filteredapiexportindexers.IndexFilteredAPIExportEndpointSliceByAPIExport,
	})
	indexers.AddIfNotPresentOrDie(apiBindingInformer.Informer().GetIndexer(), cache.Indexers{
		indexers.APIBindingsByAPIExport: indexers.IndexAPIBindingByAPIExport,
	})
}
