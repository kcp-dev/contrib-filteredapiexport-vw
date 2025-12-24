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

package reconciler

import (
	"context"
	"fmt"
	"time"

	kcpclientset "github.com/kcp-dev/sdk/client/clientset/versioned/cluster"
	kcpinformers "github.com/kcp-dev/sdk/client/informers/externalversions"
	apisv1alpha2informers "github.com/kcp-dev/sdk/client/informers/externalversions/apis/v1alpha2"

	filteredapiexportindexers "github.com/kcp-dev/contrib-filteredapiexport-vw/indexers"
	"github.com/kcp-dev/contrib-filteredapiexport-vw/reconciler/endpointslice"
	"github.com/kcp-dev/contrib-filteredapiexport-vw/reconciler/endpointsliceurls"
	filteredapiexportclientset "github.com/kcp-dev/contrib-filteredapiexport-vw/sdk/clientset/versioned/cluster"
	filteredapiexportinformers "github.com/kcp-dev/contrib-filteredapiexport-vw/sdk/informers/externalversions"

	"github.com/kcp-dev/kcp/pkg/indexers"
	"github.com/kcp-dev/kcp/pkg/reconciler/apis/apiexportendpointslice"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

type RunFunc func(ctx context.Context)
type WaitFunc func(ctx context.Context, r *Reconciler) error

const (
	waitPollInterval = time.Millisecond * 100
)

type Reconciler struct {
	ShardName                             string
	KcpClusterClient                      kcpclientset.ClusterInterface
	CacheKcpSharedInformerFactory         kcpinformers.SharedInformerFactory
	CacheFilteredAPIExportInformerFactory filteredapiexportinformers.SharedInformerFactory
	LocalKcpSharedInformerFactory         kcpinformers.SharedInformerFactory
	FilteredAPIExportInformerFactory      filteredapiexportinformers.SharedInformerFactory

	syncedCh    chan struct{}
	controllers map[string]*controllerWrapper
}

func NewReconciler(
	shardName string,
	kcpClusterClient kcpclientset.ClusterInterface,
	cacheKcpSharedInformerFactory kcpinformers.SharedInformerFactory,
	cacheFilteredAPIExportInformerFactory filteredapiexportinformers.SharedInformerFactory,
	localKcpSharedInformerFactory kcpinformers.SharedInformerFactory,
	filteredAPIExportInformerFactory filteredapiexportinformers.SharedInformerFactory,
) *Reconciler {
	return &Reconciler{
		ShardName:                             shardName,
		KcpClusterClient:                      kcpClusterClient,
		CacheKcpSharedInformerFactory:         cacheKcpSharedInformerFactory,
		CacheFilteredAPIExportInformerFactory: cacheFilteredAPIExportInformerFactory,
		LocalKcpSharedInformerFactory:         localKcpSharedInformerFactory,
		FilteredAPIExportInformerFactory:      filteredAPIExportInformerFactory,
		syncedCh:                              make(chan struct{}),
		controllers:                           make(map[string]*controllerWrapper),
	}
}

type controllerWrapper struct {
	Name   string
	Runner RunFunc
	Wait   WaitFunc
}

func (r *Reconciler) InstallControllers(ctx context.Context, config *rest.Config) error {
	if err := r.installFilteredAPIExportEndpointSliceController(ctx, config); err != nil {
		return fmt.Errorf("failed to install FilteredAPIExportEndpointSlice controller: %w", err)
	}

	if err := r.installFilteredAPIExportEndpointSliceURLsController(ctx, config); err != nil {
		return fmt.Errorf("failed to install FilteredAPIExportEndpointSliceURLs controller: %w", err)
	}

	return nil
}

func (r *Reconciler) InstallIndexers(
	filteredAPIExportEndpointSliceClusterInformer filteredapiexportinformers.SharedInformerFactory,
	globalAPIExportClusterInformer apisv1alpha2informers.APIExportClusterInformer,
) {
	endpointslice.InstallIndexers(globalAPIExportClusterInformer, filteredAPIExportEndpointSliceClusterInformer.Filteredapiexport().V1alpha1().FilteredAPIExportEndpointSlices())

	// Install indexers for endpointsliceurls controller
	endpointsliceurls.InstallIndexers(
		r.CacheFilteredAPIExportInformerFactory.Filteredapiexport().V1alpha1().FilteredAPIExportEndpointSlices(),
		r.FilteredAPIExportInformerFactory.Filteredapiexport().V1alpha1().FilteredAPIExportEndpointSlices(),
		r.LocalKcpSharedInformerFactory.Apis().V1alpha2().APIBindings(),
	)

	// Install common indexers
	indexers.AddIfNotPresentOrDie(r.CacheFilteredAPIExportInformerFactory.Filteredapiexport().V1alpha1().FilteredAPIExportEndpointSlices().Informer().GetIndexer(), cache.Indexers{
		filteredapiexportindexers.FilteredAPIExportEndpointSliceByAPIExport: filteredapiexportindexers.IndexFilteredAPIExportEndpointSliceByAPIExport,
	})
}

func (r *Reconciler) StartControllers(ctx context.Context) {
	for _, controller := range r.controllers {
		go r.runController(ctx, controller)
	}
}

func (r *Reconciler) runController(ctx context.Context, controller *controllerWrapper) {
	log := klog.FromContext(ctx).WithValues("controller", controller.Name)
	log.Info("waiting for sync")

	// controllers can define their own custom wait functions in case
	// they need to start early. If they do not define one, we will wait
	// for everything to sync.
	var err error
	if controller.Wait != nil {
		err = controller.Wait(ctx, r)
	}
	if err != nil {
		log.Error(err, "failed to wait for sync")
		return
	}

	log.Info("starting registered controller")
	controller.Runner(ctx)
}

func (r *Reconciler) registerController(controller *controllerWrapper) error {
	if r.controllers[controller.Name] != nil {
		return fmt.Errorf("controller %s is already registered", controller.Name)
	}

	r.controllers[controller.Name] = controller

	return nil
}

func (r *Reconciler) installFilteredAPIExportEndpointSliceController(_ context.Context, config *rest.Config) error {
	config = rest.CopyConfig(config)
	config = rest.AddUserAgent(config, endpointslice.ControllerName)

	filteredAPIExportClusterClient, err := filteredapiexportclientset.NewForConfig(config)
	if err != nil {
		return err
	}

	c, err := endpointslice.NewController(
		r.FilteredAPIExportInformerFactory.Filteredapiexport().V1alpha1().FilteredAPIExportEndpointSlices(),
		filteredAPIExportClusterClient,
		// Shards and APIExports get retrieved from cache server
		r.CacheKcpSharedInformerFactory.Apis().V1alpha2().APIExports(),
	)
	if err != nil {
		return err
	}

	return r.registerController(&controllerWrapper{
		Name: apiexportendpointslice.ControllerName,
		Wait: func(ctx context.Context, r *Reconciler) error {
			return wait.PollUntilContextCancel(ctx, waitPollInterval, true, func(ctx context.Context) (bool, error) {
				return r.FilteredAPIExportInformerFactory.Filteredapiexport().V1alpha1().FilteredAPIExportEndpointSlices().Informer().HasSynced() &&
					r.CacheKcpSharedInformerFactory.Apis().V1alpha2().APIExports().Informer().HasSynced(), nil
			})
		},
		Runner: func(ctx context.Context) {
			c.Start(ctx, 2)
		},
	})
}

func (r *Reconciler) installFilteredAPIExportEndpointSliceURLsController(_ context.Context, config *rest.Config) error {
	config = rest.CopyConfig(config)
	config = rest.AddUserAgent(config, endpointsliceurls.ControllerName)

	filteredAPIExportClusterClient, err := filteredapiexportclientset.NewForConfig(config)
	if err != nil {
		return err
	}

	c, err := endpointsliceurls.NewController(
		r.ShardName,
		r.FilteredAPIExportInformerFactory.Filteredapiexport().V1alpha1().FilteredAPIExportEndpointSlices(),
		r.CacheFilteredAPIExportInformerFactory.Filteredapiexport().V1alpha1().FilteredAPIExportEndpointSlices(),
		filteredAPIExportClusterClient,
		r.LocalKcpSharedInformerFactory.Apis().V1alpha2().APIBindings(),
		r.CacheKcpSharedInformerFactory.Apis().V1alpha2().APIExports(),
		r.CacheKcpSharedInformerFactory.Core().V1alpha1().Shards(),
		r.KcpClusterClient,
	)
	if err != nil {
		return err
	}

	return r.registerController(&controllerWrapper{
		Name: endpointsliceurls.ControllerName,
		Wait: func(ctx context.Context, r *Reconciler) error {
			return wait.PollUntilContextCancel(ctx, waitPollInterval, true, func(ctx context.Context) (bool, error) {
				return r.FilteredAPIExportInformerFactory.Filteredapiexport().V1alpha1().FilteredAPIExportEndpointSlices().Informer().HasSynced() &&
					r.LocalKcpSharedInformerFactory.Apis().V1alpha2().APIBindings().Informer().HasSynced() &&
					r.CacheFilteredAPIExportInformerFactory.Filteredapiexport().V1alpha1().FilteredAPIExportEndpointSlices().Informer().HasSynced() &&
					r.CacheKcpSharedInformerFactory.Core().V1alpha1().Shards().Informer().HasSynced() &&
					r.CacheKcpSharedInformerFactory.Apis().V1alpha2().APIExports().Informer().HasSynced(), nil
			})
		},
		Runner: func(ctx context.Context) {
			c.Start(ctx, 2)
		},
	})
}
