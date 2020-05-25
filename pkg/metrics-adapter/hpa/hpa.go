package custom

import (
	"context"
	"reflect"
	"sync"
	"time"

	"github.com/kaladaOpuiyo/k8s-logicmonitor-adapter/pkg/collector"
	m "github.com/kaladaOpuiyo/k8s-logicmonitor-adapter/pkg/metrics-adapter/metrics-store"
	"github.com/kaladaOpuiyo/k8s-logicmonitor-adapter/pkg/recorder"
	"github.com/kubernetes-incubator/custom-metrics-apiserver/pkg/provider"
	log "github.com/sirupsen/logrus"
	autoscalingv2 "k8s.io/api/autoscaling/v2beta2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	kube_record "k8s.io/client-go/tools/record"
	"k8s.io/metrics/pkg/apis/external_metrics"
)

type metricCollection struct {
	Values []collector.CollectedMetric
	Error  error
}
type resourceReference struct {
	Name      string
	Namespace string
}

// CollectorScheduler is a scheduler for running metric collection jobs.
// It keeps track of all running collectors and stops them if they are to be
// removed.
type CollectorScheduler struct {
	ctx        context.Context
	table      map[resourceReference]map[collector.MetricTypeName]context.CancelFunc
	metricSink chan<- metricCollection
	sync.RWMutex
}

// HPAProvider is a base provider for initializing metric collectors based on
// HPA resources.
type HPAProvider struct {
	client             kubernetes.Interface
	interval           time.Duration
	collectorScheduler *CollectorScheduler
	collectorInterval  time.Duration
	metricSink         chan metricCollection
	hpaCache           map[resourceReference]autoscalingv2.HorizontalPodAutoscaler
	metricStore        *m.MetricStore
	collectorFactory   *collector.CollectorFactory
	recorder           kube_record.EventRecorder
	logger             *log.Entry
}

// NewHPAProvider ...
func NewHPAProvider(client kubernetes.Interface, interval, collectorInterval time.Duration, collectorFactory *collector.CollectorFactory) *HPAProvider {
	metricsc := make(chan metricCollection)

	return &HPAProvider{
		client:            client,
		interval:          interval,
		collectorInterval: collectorInterval,
		metricSink:        metricsc,
		metricStore: m.NewMetricStore(func() time.Time {
			return time.Now().UTC().Add(15 * time.Minute)
		}),
		collectorFactory: collectorFactory,
		recorder:         recorder.CreateEventRecorder(client),
		logger:           log.WithFields(log.Fields{"provider": "hpa"}),
	}
}

// Run runs the HPA resource discovery and metric collection.
func (h *HPAProvider) Run(ctx context.Context) {
	// initialize collector table
	h.collectorScheduler = NewCollectorScheduler(ctx, h.metricSink)

	go h.collectMetrics(ctx)

	for {
		err := h.update()
		if err != nil {
			h.logger.Error(err)
		}

		select {
		case <-time.After(h.interval):
		case <-ctx.Done():
			h.logger.Info("Stopped HPA provider.")
			return
		}
	}
}

// update discovers all HPA resources and sets upmetric collectors for new
// HPAs.
func (h *HPAProvider) update() error {
	h.logger.Info("Looking for HPAs")

	hpas, err := h.client.AutoscalingV2beta2().HorizontalPodAutoscalers(metav1.NamespaceAll).List(metav1.ListOptions{})
	if err != nil {
		return err
	}

	newHPACache := make(map[resourceReference]autoscalingv2.HorizontalPodAutoscaler, len(hpas.Items))

	newHPAs := 0

	for _, hpa := range hpas.Items {

		resourceRef := resourceReference{
			Name:      hpa.Name,
			Namespace: hpa.Namespace,
		}

		cachedHPA, ok := h.hpaCache[resourceRef]
		hpaUpdated := !equalHPA(cachedHPA, hpa)
		if !ok || hpaUpdated {
			// if the hpa has changed then remove the previous
			// scheduled collector.
			if hpaUpdated {
				h.logger.Infof("Removing previously scheduled metrics collector: %s", resourceRef)
				h.collectorScheduler.Remove(resourceRef)
			}

			metricConfigs, err := collector.Parse(&hpa)
			if err != nil {
				h.logger.Errorf("Failed to parse HPA metrics: %v", err)
				continue
			}

			cache := true
			for _, config := range metricConfigs {
				interval := config.Interval
				if interval == 0 {
					interval = h.collectorInterval
				}

				c, err := h.collectorFactory.NewCollector(&hpa, config, interval)
				if err != nil {

					cache = false
					continue
				}

				h.logger.Infof("Adding new metrics collector: %T", c)
				h.collectorScheduler.Add(resourceRef, config.MetricTypeName, c)
			}
			newHPAs++

			// if we get an error setting upthe collectors for the
			// HPA, don't cache it, but try again later.
			if !cache {
				continue
			}
		}

		newHPACache[resourceRef] = hpa
	}

	for ref := range h.hpaCache {
		if _, ok := newHPACache[ref]; ok {
			continue
		}

		h.logger.Infof("Removing previously scheduled metrics collector: %s", ref)
		h.collectorScheduler.Remove(ref)
	}

	h.logger.Infof("Found %d new/updated HPA(s)", newHPAs)
	h.hpaCache = newHPACache
	return nil
}

// equalHPA returns true if two HPAs are identical (apart from their status).
func equalHPA(a, b autoscalingv2.HorizontalPodAutoscaler) bool {
	// reset resource version to not compare it since this will change
	// whenever the status of the object is updated. We only want to
	// compare the metadata and the spec.
	a.ObjectMeta.ResourceVersion = ""
	b.ObjectMeta.ResourceVersion = ""
	return reflect.DeepEqual(a.ObjectMeta, b.ObjectMeta) && reflect.DeepEqual(a.Spec, b.Spec)
}

// collectMetrics collects all metrics from collectors and manages a central
// metric store.
func (h *HPAProvider) collectMetrics(ctx context.Context) {
	// run garbage collection every 10 minutes
	go func(ctx context.Context) {
		for {
			select {
			case <-time.After(10 * time.Minute):
				h.metricStore.RemoveExpired()
			case <-ctx.Done():
				h.logger.Info("Stopped metrics store garbage collection.")
				return
			}
		}
	}(ctx)

	for {
		select {
		case collection := <-h.metricSink:
			if collection.Error != nil {
				h.logger.Errorf("Failed to collect metrics: %v", collection.Error)
			}

			h.logger.Infof("Collected %d new metric(s)", len(collection.Values))
			for _, value := range collection.Values {
				switch value.Type {
				case autoscalingv2.ExternalMetricSourceType:
					h.logger.Infof("Collected new external metric '%s' (%s) [%s]",
						value.External.MetricName,
						value.External.Value.String(),
						labels.Set(value.External.MetricLabels).String(),
					)
				}
				h.metricStore.Insert(value)
			}
		case <-ctx.Done():
			h.logger.Info("Stopped metrics collection.")
			return
		}
	}
}

// GetExternalMetric ...
func (h *HPAProvider) GetExternalMetric(namespace string, metricSelector labels.Selector, info provider.ExternalMetricInfo) (*external_metrics.ExternalMetricValueList, error) {
	return h.metricStore.GetExternalMetric(namespace, metricSelector, info)
}

// ListAllExternalMetrics ...
func (h *HPAProvider) ListAllExternalMetrics() []provider.ExternalMetricInfo {
	return h.metricStore.ListAllExternalMetrics()
}

// NewCollectorScheduler initializes a new CollectorScheduler.
func NewCollectorScheduler(ctx context.Context, metricsc chan<- metricCollection) *CollectorScheduler {
	return &CollectorScheduler{
		ctx:        ctx,
		table:      map[resourceReference]map[collector.MetricTypeName]context.CancelFunc{},
		metricSink: metricsc,
	}
}

// Add adds a new collector to the collector scheduler. Once the collector is
// added it will be started to collect metrics.
func (t *CollectorScheduler) Add(resourceRef resourceReference, typeName collector.MetricTypeName, metricCollector collector.Collector) {
	t.Lock()
	defer t.Unlock()

	collectors, ok := t.table[resourceRef]
	if !ok {
		collectors = map[collector.MetricTypeName]context.CancelFunc{}
		t.table[resourceRef] = collectors
	}

	if cancelCollector, ok := collectors[typeName]; ok {
		// stopold collector
		cancelCollector()
	}

	ctx, cancel := context.WithCancel(t.ctx)
	collectors[typeName] = cancel

	// start runner for new collector
	go collectorRunner(ctx, metricCollector, t.metricSink)
}

// collectorRunner runs a collector at the desired interval. If the passed
// context is canceled the collection will be stopped.
func collectorRunner(ctx context.Context, collector collector.Collector, metricsc chan<- metricCollection) {
	for {
		values, err := collector.GetMetrics()

		metricsc <- metricCollection{
			Values: values,
			Error:  err,
		}

		select {
		case <-time.After(collector.Interval()):
		case <-ctx.Done():
			log.Info("stopping collector runner...")
			return
		}
	}
}

// Remove removes a collector from the Collector scheduler. The collector is
// stopped before it's removed.
func (t *CollectorScheduler) Remove(resourceRef resourceReference) {
	t.Lock()
	defer t.Unlock()

	if collectors, ok := t.table[resourceRef]; ok {
		for _, cancelCollector := range collectors {
			cancelCollector()
		}
		delete(t.table, resourceRef)
	}
}
