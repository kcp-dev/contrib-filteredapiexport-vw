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

package apireconciler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
	kcpcache "github.com/kcp-dev/apimachinery/v2/pkg/cache"
	"github.com/kcp-dev/logicalcluster/v3"
	apisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	apisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	kcpclientset "github.com/kcp-dev/sdk/client/clientset/versioned/cluster"
	apisv1alpha1informers "github.com/kcp-dev/sdk/client/informers/externalversions/apis/v1alpha1"
	apisv1alpha2informers "github.com/kcp-dev/sdk/client/informers/externalversions/apis/v1alpha2"
	apisv1alpha1listers "github.com/kcp-dev/sdk/client/listers/apis/v1alpha1"
	apisv1alpha2listers "github.com/kcp-dev/sdk/client/listers/apis/v1alpha2"

	filteredapiexportv1alpha1 "github.com/kcp-dev/contrib-filteredapiexport-vw/sdk/apis/filteredapiexport/v1alpha1"
	filteredapiexportinformers "github.com/kcp-dev/contrib-filteredapiexport-vw/sdk/informers/externalversions/filteredapiexport/v1alpha1"
	filteredapiexportlisters "github.com/kcp-dev/contrib-filteredapiexport-vw/sdk/listers/filteredapiexport/v1alpha1"

	"github.com/kcp-dev/kcp/pkg/indexers"
	"github.com/kcp-dev/kcp/pkg/logging"
	"github.com/kcp-dev/kcp/pkg/reconciler/events"
	"github.com/kcp-dev/kcp/pkg/virtual/framework/dynamic/apidefinition"
	dynamiccontext "github.com/kcp-dev/kcp/pkg/virtual/framework/dynamic/context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

const (
	ControllerName = "kcp-virtual-filtered-apiexport-api-reconciler"
)

type CreateAPIDefinitionFunc func(apiResourceSchema *apisv1alpha1.APIResourceSchema, version string, identityHash string, additionalLabelRequirements labels.Requirements) (apidefinition.APIDefinition, error)

// NewAPIReconciler returns a new controller which reconciles FilteredAPIExportEndpointSlice resources
// and delegates the corresponding SyncTargetAPI management to the given SyncTargetAPIManager.
func NewAPIReconciler(
	kcpClusterClient kcpclientset.ClusterInterface,
	filteredAPIExportESInformer filteredapiexportinformers.FilteredAPIExportEndpointSliceClusterInformer,
	apiResourceSchemaInformer apisv1alpha1informers.APIResourceSchemaClusterInformer,
	apiExportInformer apisv1alpha2informers.APIExportClusterInformer,
	createAPIDefinition CreateAPIDefinitionFunc,
	createAPIBindingAPIDefinition func(ctx context.Context, apibindingVersion string, clusterName logicalcluster.Name, apiExportName string) (apidefinition.APIDefinition, error),
	createFilteredAPIExportESAPIDefinition func(ctx context.Context, apiVersion string, clusterName logicalcluster.Name, apiExportName string) (apidefinition.APIDefinition, error),
) (*APIReconciler, error) {
	c := &APIReconciler{
		kcpClusterClient: kcpClusterClient,

		filteredAPIExportEndpointSliceLister:  filteredAPIExportESInformer.Lister(),
		filteredAPIExportEndpointSliceIndexer: filteredAPIExportESInformer.Informer().GetIndexer(),
		listFilteredAPIExportEndpointSlices: func(clusterName logicalcluster.Name) ([]*filteredapiexportv1alpha1.FilteredAPIExportEndpointSlice, error) {
			return filteredAPIExportESInformer.Lister().Cluster(clusterName).List(labels.Everything())
		},

		apiResourceSchemaLister:  apiResourceSchemaInformer.Lister(),
		apiResourceSchemaIndexer: apiResourceSchemaInformer.Informer().GetIndexer(),

		apiExportLister:  apiExportInformer.Lister(),
		apiExportIndexer: apiExportInformer.Informer().GetIndexer(),
		listAPIExports: func(clusterName logicalcluster.Name) ([]*apisv1alpha2.APIExport, error) {
			return apiExportInformer.Lister().Cluster(clusterName).List(labels.Everything())
		},
		getAPIExport: func(clusterName logicalcluster.Name, name string) (*apisv1alpha2.APIExport, error) {
			return apiExportInformer.Lister().Cluster(clusterName).Get(name)
		},

		queue: workqueue.NewTypedRateLimitingQueueWithConfig(
			workqueue.DefaultTypedControllerRateLimiter[string](),
			workqueue.TypedRateLimitingQueueConfig[string]{
				Name: ControllerName,
			},
		),

		createAPIDefinition:                    createAPIDefinition,
		createAPIBindingAPIDefinition:          createAPIBindingAPIDefinition,
		createFilteredAPIExportESAPIDefinition: createFilteredAPIExportESAPIDefinition,

		apiSets: map[dynamiccontext.APIDomainKey]apidefinition.APIDefinitionSet{},
	}

	indexers.AddIfNotPresentOrDie(
		apiExportInformer.Informer().GetIndexer(),
		cache.Indexers{
			indexers.APIExportByIdentity:          indexers.IndexAPIExportByIdentity,
			indexers.APIExportByClaimedIdentities: indexers.IndexAPIExportByClaimedIdentities,
		},
	)

	logger := logging.WithReconciler(klog.Background(), ControllerName)

	_, _ = filteredAPIExportESInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			c.enqueueFilteredAPIExportEndpointSlice(obj.(*filteredapiexportv1alpha1.FilteredAPIExportEndpointSlice), logger)
		},
		UpdateFunc: func(_, obj interface{}) {
			c.enqueueFilteredAPIExportEndpointSlice(obj.(*filteredapiexportv1alpha1.FilteredAPIExportEndpointSlice), logger)
		},
		DeleteFunc: func(obj interface{}) {
			c.enqueueFilteredAPIExportEndpointSlice(obj.(*filteredapiexportv1alpha1.FilteredAPIExportEndpointSlice), logger)
		},
	})

	_, _ = apiExportInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			c.enqueueAPIExport(obj.(*apisv1alpha2.APIExport), logger)
		},
		UpdateFunc: func(_, obj interface{}) {
			c.enqueueAPIExport(obj.(*apisv1alpha2.APIExport), logger)
		},
		DeleteFunc: func(obj interface{}) {
			c.enqueueAPIExport(obj.(*apisv1alpha2.APIExport), logger)
		},
	})

	_, _ = apiResourceSchemaInformer.Informer().AddEventHandler(events.WithoutSyncs(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			c.enqueueAPIResourceSchema(obj.(*apisv1alpha1.APIResourceSchema), logger)
		},
		DeleteFunc: func(obj interface{}) {
			c.enqueueAPIResourceSchema(obj.(*apisv1alpha1.APIResourceSchema), logger)
		},
	}))

	return c, nil
}

// APIReconciler is a controller watching APIExports and APIResourceSchemas, and updates the
// API definitions driving the virtual workspace.
type APIReconciler struct {
	kcpClusterClient kcpclientset.ClusterInterface

	filteredAPIExportEndpointSliceLister  filteredapiexportlisters.FilteredAPIExportEndpointSliceClusterLister
	filteredAPIExportEndpointSliceIndexer cache.Indexer
	listFilteredAPIExportEndpointSlices   func(clusterName logicalcluster.Name) ([]*filteredapiexportv1alpha1.FilteredAPIExportEndpointSlice, error)

	apiResourceSchemaLister  apisv1alpha1listers.APIResourceSchemaClusterLister
	apiResourceSchemaIndexer cache.Indexer

	apiExportLister  apisv1alpha2listers.APIExportClusterLister
	apiExportIndexer cache.Indexer
	listAPIExports   func(clusterName logicalcluster.Name) ([]*apisv1alpha2.APIExport, error)
	getAPIExport     func(clusterName logicalcluster.Name, name string) (*apisv1alpha2.APIExport, error)

	queue workqueue.TypedRateLimitingInterface[string]

	createAPIDefinition                    CreateAPIDefinitionFunc
	createAPIBindingAPIDefinition          func(ctx context.Context, apibindingVersion string, clusterName logicalcluster.Name, apiExportName string) (apidefinition.APIDefinition, error)
	createFilteredAPIExportESAPIDefinition func(ctx context.Context, apiVersion string, clusterName logicalcluster.Name, apiExportName string) (apidefinition.APIDefinition, error)

	mutex   sync.RWMutex // protects the map, not the values!
	apiSets map[dynamiccontext.APIDomainKey]apidefinition.APIDefinitionSet
}

func (c *APIReconciler) enqueueFilteredAPIExportEndpointSlice(filteredSlice *filteredapiexportv1alpha1.FilteredAPIExportEndpointSlice, logger logr.Logger) {
	key, err := kcpcache.DeletionHandlingMetaClusterNamespaceKeyFunc(filteredSlice)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}
	logging.WithQueueKey(logger, key).V(4).Info("queueing FilteredAPIExportEndpointSlice")
	c.queue.Add(key)

	apiExport, err := c.getAPIExport(logicalcluster.Name(filteredSlice.Spec.APIExport.Path), filteredSlice.Spec.APIExport.Name)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}

	if apiExport.Status.IdentityHash != "" {
		logger.V(4).Info("looking for APIExports to queue that have claims against this identity", "identity", apiExport.Status.IdentityHash)
		others, err := indexers.ByIndex[*apisv1alpha2.APIExport](c.apiExportIndexer, indexers.APIExportByClaimedIdentities, apiExport.Status.IdentityHash)
		if err != nil {
			logger.Error(err, "error getting APIExports for claimed identity", "identity", apiExport.Status.IdentityHash)
			return
		}
		logger.V(4).Info("got APIExports", "identity", apiExport.Status.IdentityHash, "count", len(others))
		for _, other := range others {
			filteredSlices, err := c.filteredAPIExportEndpointSliceLister.Cluster(logicalcluster.From(other)).List(labels.Everything())
			if err != nil {
				logger.Error(err, "error listing FilteredAPIExportEndpointSlices")
				return
			}
			for _, filteredSlice := range filteredSlices {
				if filteredSlice.Spec.APIExport.Name == other.Name {
					key, err := kcpcache.MetaClusterNamespaceKeyFunc(filteredSlice)
					if err != nil {
						logger.Error(err, "error getting key!")
						continue
					}
					logging.WithQueueKey(logger, key).V(4).Info("queueing FilteredAPIExportEndpointSlice for APIExport claim")
					c.queue.Add(key)
				}
			}
		}
	}
}

func (c *APIReconciler) enqueueAPIResourceSchema(apiResourceSchema *apisv1alpha1.APIResourceSchema, logger logr.Logger) {
	key, err := kcpcache.DeletionHandlingMetaClusterNamespaceKeyFunc(apiResourceSchema)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}

	clusterName, _, _, err := kcpcache.SplitMetaClusterNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}

	filteredSlices, err := c.listFilteredAPIExportEndpointSlices(clusterName)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}

	logger = logging.WithObject(logger, apiResourceSchema)

	if len(filteredSlices) == 0 {
		logger.V(3).Info("No FilteredAPIExportEndpointSlices found")
		return
	}

	for _, filteredSlice := range filteredSlices {
		apiExport, err := c.getAPIExport(logicalcluster.Name(filteredSlice.Spec.APIExport.Path), filteredSlice.Spec.APIExport.Name)
		if err != nil {
			utilruntime.HandleError(err)
			return
		}
		if apiExport == nil {
			continue
		}
		for _, res := range apiExport.Spec.Resources {
			if res.Schema == apiResourceSchema.Name {
				logger.WithValues("filteredapiexportendpointslice", filteredSlice.Name).V(4).Info("Queueing FilteredAPIExportEndpointSlice for APIResourceSchema")
				c.enqueueFilteredAPIExportEndpointSlice(filteredSlice, logger.WithValues("reason", "APIResourceSchema change", "apiResourceSchema", apiResourceSchema.Name))
			}
		}
	}
}

func (c *APIReconciler) enqueueAPIExport(apiExport *apisv1alpha2.APIExport, logger logr.Logger) {
	key, err := kcpcache.DeletionHandlingMetaClusterNamespaceKeyFunc(apiExport)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}

	clusterName, _, _, err := kcpcache.SplitMetaClusterNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}

	filteredSlices, err := c.listFilteredAPIExportEndpointSlices(clusterName)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}

	logger = logging.WithObject(logger, apiExport)

	if len(filteredSlices) == 0 {
		logger.V(3).Info("No FilteredAPIExportEndpointSlices found")
		return
	}

	for _, filteredSlice := range filteredSlices {
		if filteredSlice.Spec.APIExport.Name == apiExport.Name {
			logger.WithValues("filteredapiexportendpointslice", filteredSlice.Name).V(4).Info("Queueing FilteredAPIExportEndpointSlice for APIExport")
			c.enqueueFilteredAPIExportEndpointSlice(filteredSlice, logger.WithValues("reason", "APIExport change", "apiExport", apiExport.Name))
		}
	}
}

func (c *APIReconciler) startWorker(ctx context.Context) {
	for c.processNextWorkItem(ctx) {
	}
}

func (c *APIReconciler) Start(ctx context.Context) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	logger := logging.WithReconciler(klog.FromContext(ctx), ControllerName)
	ctx = klog.NewContext(ctx, logger)
	logger.Info("starting controller")
	defer logger.Info("shutting down controller")

	go wait.Until(func() { c.startWorker(ctx) }, time.Second, ctx.Done())

	// stop all watches if the controller is stopped
	defer func() {
		c.mutex.Lock()
		defer c.mutex.Unlock()
		for _, sets := range c.apiSets {
			for _, v := range sets {
				v.TearDown()
			}
		}
	}()

	<-ctx.Done()
}

func (c *APIReconciler) ShutDown() {
	c.queue.ShutDown()
}

func (c *APIReconciler) processNextWorkItem(ctx context.Context) bool {
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
		utilruntime.HandleError(fmt.Errorf("%s: failed to sync %q, err: %w", ControllerName, key, err))
		c.queue.AddRateLimited(key)
		return true
	}

	c.queue.Forget(key)
	return true
}

func (c *APIReconciler) process(ctx context.Context, key string) error {
	clusterName, _, filteredAPIExportESName, err := kcpcache.SplitMetaClusterNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(err)
		return nil
	}
	apiDomainKey := dynamiccontext.APIDomainKey(clusterName.String() + "/" + filteredAPIExportESName)

	logger := klog.FromContext(ctx).WithValues("apiDomainKey", apiDomainKey)

	filteredAPIExportES, err := c.filteredAPIExportEndpointSliceLister.Cluster(clusterName).Get(filteredAPIExportESName)
	if err != nil && !apierrors.IsNotFound(err) {
		logger.Error(err, "error getting FilteredAPIExportEndpointSlice")
		return nil // nothing we can do here
	}

	if filteredAPIExportES != nil {
		logger = logging.WithObject(logger, filteredAPIExportES)
	}
	ctx = klog.NewContext(ctx, logger)

	return c.reconcile(ctx, filteredAPIExportES, apiDomainKey)
}

func (c *APIReconciler) GetAPIDefinitionSet(_ context.Context, key dynamiccontext.APIDomainKey) (apidefinition.APIDefinitionSet, bool, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	apiSet, ok := c.apiSets[key]
	return apiSet, ok, nil
}
