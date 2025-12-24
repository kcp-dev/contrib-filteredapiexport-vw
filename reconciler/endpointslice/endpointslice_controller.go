/*
Copyright 2022 The KCP Authors.

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

package endpointslice

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	kcpcache "github.com/kcp-dev/apimachinery/v2/pkg/cache"
	"github.com/kcp-dev/logicalcluster/v3"
	apisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	"github.com/kcp-dev/sdk/apis/core"
	apisv1alpha2informers "github.com/kcp-dev/sdk/client/informers/externalversions/apis/v1alpha2"

	filteredapiexportindexers "github.com/kcp-dev/contrib-filteredapiexport-vw/indexers"
	filteredapiexportv1alpha1 "github.com/kcp-dev/contrib-filteredapiexport-vw/sdk/apis/filteredapiexport/v1alpha1"
	filteredapiexportclientset "github.com/kcp-dev/contrib-filteredapiexport-vw/sdk/clientset/versioned/cluster"
	filteredapiexportclient "github.com/kcp-dev/contrib-filteredapiexport-vw/sdk/clientset/versioned/typed/filteredapiexport/v1alpha1"
	filteredapiexportinformers "github.com/kcp-dev/contrib-filteredapiexport-vw/sdk/informers/externalversions/filteredapiexport/v1alpha1"

	"github.com/kcp-dev/kcp/pkg/indexers"
	"github.com/kcp-dev/kcp/pkg/logging"
	"github.com/kcp-dev/kcp/pkg/reconciler/committer"
	"github.com/kcp-dev/kcp/pkg/reconciler/events"
	"github.com/kcp-dev/kcp/pkg/tombstone"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

const (
	ControllerName = "kcp-filteredapiexport-endpointslice"
)

// NewController returns a new controller for FilteredAPIExportEndpointSlices.
// Shards and APIExports are read from the cache server.
func NewController(
	filteredAPIExportEndpointSliceClusterInformer filteredapiexportinformers.FilteredAPIExportEndpointSliceClusterInformer,
	filteredAPIExportClusterClient filteredapiexportclientset.ClusterInterface,
	globalAPIExportClusterInformer apisv1alpha2informers.APIExportClusterInformer,
) (*controller, error) {
	c := &controller{
		queue: workqueue.NewTypedRateLimitingQueueWithConfig(
			workqueue.DefaultTypedControllerRateLimiter[string](),
			workqueue.TypedRateLimitingQueueConfig[string]{
				Name: ControllerName,
			},
		),
		getFilteredAPIExportEndpointSlice: func(path logicalcluster.Path, name string) (*filteredapiexportv1alpha1.FilteredAPIExportEndpointSlice, error) {
			return indexers.ByPathAndName[*filteredapiexportv1alpha1.FilteredAPIExportEndpointSlice](filteredapiexportv1alpha1.Resource("filteredapiexportendpointslices"), filteredAPIExportEndpointSliceClusterInformer.Informer().GetIndexer(), path, name)
		},
		getAPIExport: func(path logicalcluster.Path, name string) (*apisv1alpha2.APIExport, error) {
			return indexers.ByPathAndName[*apisv1alpha2.APIExport](apisv1alpha2.Resource("apiexports"), globalAPIExportClusterInformer.Informer().GetIndexer(), path, name)
		},
		filteredAPIExportEndpointSliceClusterInformer: filteredAPIExportEndpointSliceClusterInformer,
		commit: committer.NewCommitter[*FilteredAPIExportEndpointSlice, Patcher, *FilteredAPIExportEndpointSliceSpec, *FilteredAPIExportEndpointSliceStatus](filteredAPIExportClusterClient.FilteredapiexportV1alpha1().FilteredAPIExportEndpointSlices()),
	}

	logger := logging.WithReconciler(klog.Background(), ControllerName)

	_, _ = filteredAPIExportEndpointSliceClusterInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			c.enqueueFilteredAPIExportEndpointSlice(tombstone.Obj[*filteredapiexportv1alpha1.FilteredAPIExportEndpointSlice](obj), logger, "")
		},
		UpdateFunc: func(_, newObj interface{}) {
			c.enqueueFilteredAPIExportEndpointSlice(tombstone.Obj[*filteredapiexportv1alpha1.FilteredAPIExportEndpointSlice](newObj), logger, "")
		},
		DeleteFunc: func(obj interface{}) {
			c.enqueueFilteredAPIExportEndpointSlice(tombstone.Obj[*filteredapiexportv1alpha1.FilteredAPIExportEndpointSlice](obj), logger, "")
		},
	})

	_, _ = globalAPIExportClusterInformer.Informer().AddEventHandler(events.WithoutSyncs(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			c.enqueueFilteredAPIExportEndpointSlicesForAPIExport(tombstone.Obj[*apisv1alpha2.APIExport](obj), logger)
		},
		DeleteFunc: func(obj interface{}) {
			c.enqueueFilteredAPIExportEndpointSlicesForAPIExport(tombstone.Obj[*apisv1alpha2.APIExport](obj), logger)
		},
	}))

	return c, nil
}

type FilteredAPIExportEndpointSlice = filteredapiexportv1alpha1.FilteredAPIExportEndpointSlice
type FilteredAPIExportEndpointSliceSpec = filteredapiexportv1alpha1.FilteredAPIExportEndpointSliceSpec
type FilteredAPIExportEndpointSliceStatus = filteredapiexportv1alpha1.FilteredAPIExportEndpointSliceStatus
type Patcher = filteredapiexportclient.FilteredAPIExportEndpointSliceInterface
type Resource = committer.Resource[*FilteredAPIExportEndpointSliceSpec, *FilteredAPIExportEndpointSliceStatus]
type CommitFunc = func(context.Context, *Resource, *Resource) error

// controller reconciles APIExportEndpointSlices. It ensures that the shard endpoints are populated
// in the status of every APIExportEndpointSlices.
type controller struct {
	queue workqueue.TypedRateLimitingInterface[string]

	getFilteredAPIExportEndpointSlice func(path logicalcluster.Path, name string) (*filteredapiexportv1alpha1.FilteredAPIExportEndpointSlice, error)
	getAPIExport                      func(path logicalcluster.Path, name string) (*apisv1alpha2.APIExport, error)

	filteredAPIExportEndpointSliceClusterInformer filteredapiexportinformers.FilteredAPIExportEndpointSliceClusterInformer
	commit                                        CommitFunc
}

// enqueueFilteredAPIExportEndpointSlice enqueues an FilteredAPIExportEndpointSlice.
func (c *controller) enqueueFilteredAPIExportEndpointSlice(slice *filteredapiexportv1alpha1.FilteredAPIExportEndpointSlice, logger logr.Logger, logSuffix string) {
	key, err := kcpcache.DeletionHandlingMetaClusterNamespaceKeyFunc(slice)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}

	logger.V(4).Info(fmt.Sprintf("queueing FilteredAPIExportEndpointSlice%s", logSuffix))
	c.queue.Add(key)
}

// enqueueFilteredAPIExportEndpointSlicesForAPIExport enqueues FilteredAPIExportEndpointSlices referencing a specific APIExport.
func (c *controller) enqueueFilteredAPIExportEndpointSlicesForAPIExport(export *apisv1alpha2.APIExport, logger logr.Logger) {
	// binding keys by full path
	keys := sets.New[string]()
	if path := logicalcluster.NewPath(export.Annotations[core.LogicalClusterPathAnnotationKey]); !path.Empty() {
		pathKeys, err := c.filteredAPIExportEndpointSliceClusterInformer.Informer().GetIndexer().IndexKeys(filteredapiexportindexers.FilteredAPIExportEndpointSliceByAPIExport, path.Join(export.Name).String())
		if err != nil {
			utilruntime.HandleError(err)
			return
		}
		keys.Insert(pathKeys...)
	}

	clusterKeys, err := c.filteredAPIExportEndpointSliceClusterInformer.Informer().GetIndexer().IndexKeys(filteredapiexportindexers.FilteredAPIExportEndpointSliceByAPIExport, logicalcluster.From(export).Path().Join(export.Name).String())
	if err != nil {
		utilruntime.HandleError(err)
		return
	}
	keys.Insert(clusterKeys...)

	for _, key := range sets.List[string](keys) {
		slice, exists, err := c.filteredAPIExportEndpointSliceClusterInformer.Informer().GetIndexer().GetByKey(key)
		if err != nil {
			utilruntime.HandleError(err)
			continue
		} else if !exists {
			continue
		}
		c.enqueueFilteredAPIExportEndpointSlice(tombstone.Obj[*filteredapiexportv1alpha1.FilteredAPIExportEndpointSlice](slice), logger, " because of referenced FilteredAPIExport")
	}
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

	if err := c.process(ctx, key); err != nil {
		utilruntime.HandleError(fmt.Errorf("%q controller failed to sync %q, err: %w", ControllerName, key, err))
		c.queue.AddRateLimited(key)
		return true
	}
	c.queue.Forget(key)
	return true
}

func (c *controller) process(ctx context.Context, key string) error {
	clusterName, _, name, err := kcpcache.SplitMetaClusterNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(err)
		return nil
	}
	obj, err := c.getFilteredAPIExportEndpointSlice(clusterName.Path(), name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil // object deleted before we handled it
		}
		return err
	}

	old := obj
	obj = obj.DeepCopy()

	logger := logging.WithObject(klog.FromContext(ctx), obj)
	ctx = klog.NewContext(ctx, logger)

	var errs []error
	if err := c.reconcile(ctx, obj); err != nil {
		errs = append(errs, err)
	}

	// Regardless of whether reconcile returned an error or not, always try to patch status if needed. Return the
	// reconciliation error at the end.

	// If the object being reconciled changed as a result, update it.
	oldResource := &Resource{ObjectMeta: old.ObjectMeta, Spec: &old.Spec, Status: &old.Status}
	newResource := &Resource{ObjectMeta: obj.ObjectMeta, Spec: &obj.Spec, Status: &obj.Status}

	if err := c.commit(ctx, oldResource, newResource); err != nil {
		errs = append(errs, err)
	}

	return utilerrors.NewAggregate(errs)
}

// InstallIndexers adds the additional indexers that this controller requires to the informers.
func InstallIndexers(
	globalAPIExportClusterInformer apisv1alpha2informers.APIExportClusterInformer,
	filteredAPIExportEndpointSliceClusterInformer filteredapiexportinformers.FilteredAPIExportEndpointSliceClusterInformer,
) {
	indexers.AddIfNotPresentOrDie(globalAPIExportClusterInformer.Informer().GetIndexer(), cache.Indexers{
		indexers.ByLogicalClusterPathAndName: indexers.IndexByLogicalClusterPathAndName,
	})
	indexers.AddIfNotPresentOrDie(filteredAPIExportEndpointSliceClusterInformer.Informer().GetIndexer(), cache.Indexers{
		indexers.ByLogicalClusterPathAndName: indexers.IndexByLogicalClusterPathAndName,
	})
	indexers.AddIfNotPresentOrDie(filteredAPIExportEndpointSliceClusterInformer.Informer().GetIndexer(), cache.Indexers{
		filteredapiexportindexers.FilteredAPIExportEndpointSliceByAPIExport: filteredapiexportindexers.IndexFilteredAPIExportEndpointSliceByAPIExport,
	})
}
