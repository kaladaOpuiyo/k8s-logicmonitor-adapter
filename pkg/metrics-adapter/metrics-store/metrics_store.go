package provider

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kaladaOpuiyo/k8s-logicmonitor-adapter/pkg/collector"
	"github.com/kubernetes-incubator/custom-metrics-apiserver/pkg/provider"
	autoscalingv2 "k8s.io/api/autoscaling/v2beta2"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/metrics/pkg/apis/external_metrics"
)

type externalMetricsStoredMetric struct {
	Value external_metrics.ExternalMetricValue
	TTL   time.Time
}

// MetricStore is a simple in-memory Metrics Store for HPA metrics.
type MetricStore struct {
	externalMetricsStore map[string]map[string]externalMetricsStoredMetric
	metricsTTLCalculator func() time.Time
	sync.RWMutex
}

// NewMetricStore initializes an empty Metrics Store.
func NewMetricStore(ttlCalculator func() time.Time) *MetricStore {
	return &MetricStore{
		externalMetricsStore: make(map[string]map[string]externalMetricsStoredMetric),
		metricsTTLCalculator: ttlCalculator,
	}
}

// Insert inserts a collected metric into the metric customMetricsStore.
func (s *MetricStore) Insert(value collector.CollectedMetric) {
	switch value.Type {
	case autoscalingv2.ExternalMetricSourceType:
		s.insertExternalMetric(value.External)
	}
}

// insertExternalMetric inserts an external metric into the store.
func (s *MetricStore) insertExternalMetric(metric external_metrics.ExternalMetricValue) {
	s.Lock()
	defer s.Unlock()

	storedMetric := externalMetricsStoredMetric{
		Value: metric,
		TTL:   s.metricsTTLCalculator(), // TODO: make TTL configurable
	}

	labelsKey := hashLabelMap(metric.MetricLabels)

	if metrics, ok := s.externalMetricsStore[metric.MetricName]; ok {
		metrics[labelsKey] = storedMetric
	} else {
		s.externalMetricsStore[metric.MetricName] = map[string]externalMetricsStoredMetric{
			labelsKey: storedMetric,
		}
	}
}

// hashLabelMap converts a map into a sorted string to provide a stable
// representation of a labels map.
func hashLabelMap(labels map[string]string) string {
	strLabels := make([]string, 0, len(labels))
	for k, v := range labels {
		strLabels = append(strLabels, fmt.Sprintf("%s=%s", k, v))
	}
	sort.Strings(strLabels)
	return strings.Join(strLabels, ",")
}

// GetExternalMetric gets external metric from the store by metric name and
// selector.
func (s *MetricStore) GetExternalMetric(namespace string, selector labels.Selector, info provider.ExternalMetricInfo) (*external_metrics.ExternalMetricValueList, error) {
	matchedMetrics := make([]external_metrics.ExternalMetricValue, 0)

	s.RLock()
	defer s.RUnlock()

	if metrics, ok := s.externalMetricsStore[info.Metric]; ok {
		for _, metric := range metrics {
			if selector.Matches(labels.Set(metric.Value.MetricLabels)) {
				matchedMetrics = append(matchedMetrics, metric.Value)
			}
		}
	}

	return &external_metrics.ExternalMetricValueList{Items: matchedMetrics}, nil
}

// ListAllExternalMetrics lists all external metrics in the Metrics Store.
func (s *MetricStore) ListAllExternalMetrics() []provider.ExternalMetricInfo {
	s.RLock()
	defer s.RUnlock()

	metricsInfo := make([]provider.ExternalMetricInfo, 0, len(s.externalMetricsStore))

	for metricName := range s.externalMetricsStore {
		info := provider.ExternalMetricInfo{
			Metric: metricName,
		}
		metricsInfo = append(metricsInfo, info)
	}
	return metricsInfo
}

// RemoveExpired removes expired metrics from the Metrics Store. A metric is
// considered expired if its metricsTTL is before time.Now().
func (s *MetricStore) RemoveExpired() {
	s.Lock()
	defer s.Unlock()

	// cleanup external metrics
	for metricName, metrics := range s.externalMetricsStore {
		for k, metric := range metrics {
			if metric.TTL.Before(time.Now().UTC()) {
				delete(metrics, k)
			}
		}
		if len(metrics) == 0 {
			delete(s.externalMetricsStore, metricName)
		}
	}
}
