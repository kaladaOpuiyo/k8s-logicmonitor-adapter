package collector

import (
	"fmt"
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2beta2"
	"k8s.io/metrics/pkg/apis/external_metrics"
)

type CollectorFactory struct {
	externalPlugins map[string]CollectorPlugin
}

func NewCollectorFactory() *CollectorFactory {
	return &CollectorFactory{
		externalPlugins: map[string]CollectorPlugin{},
	}
}

type CollectorPlugin interface {
	NewCollector(hpa *autoscalingv2.HorizontalPodAutoscaler, config *MetricConfig, interval time.Duration) (Collector, error)
}

type PluginNotFoundError struct {
	metricTypeName MetricTypeName
}

func (p *PluginNotFoundError) Error() string {
	return fmt.Sprintf("no plugin found for %s", p.metricTypeName)
}

func (c *CollectorFactory) RegisterExternalCollector(metrics []string, plugin CollectorPlugin) {
	for _, metric := range metrics {
		c.externalPlugins[metric] = plugin
	}
}

func (c *CollectorFactory) NewCollector(hpa *autoscalingv2.HorizontalPodAutoscaler, config *MetricConfig, interval time.Duration) (Collector, error) {
	switch config.Type {
	case autoscalingv2.ExternalMetricSourceType:
		if plugin, ok := c.externalPlugins[config.Metric.Name]; ok {
			return plugin.NewCollector(hpa, config, interval)
		}
	default:
	}

	return nil, &PluginNotFoundError{metricTypeName: config.MetricTypeName}
}

type MetricTypeName struct {
	Type   autoscalingv2.MetricSourceType
	Metric autoscalingv2.MetricIdentifier
}

type CollectedMetric struct {
	Type     autoscalingv2.MetricSourceType
	External external_metrics.ExternalMetricValue
}

type Collector interface {
	GetMetrics() ([]CollectedMetric, error)
	Interval() time.Duration
}

type MetricConfig struct {
	MetricTypeName
	CollectorName string
	Config        map[string]string
	PerReplica    bool
	Interval      time.Duration
	MetricSpec    autoscalingv2.MetricSpec
}

// Parse parses the HPA object into a list of metric configurations.
func Parse(hpa *autoscalingv2.HorizontalPodAutoscaler) ([]*MetricConfig, error) {
	metricConfigs := make([]*MetricConfig, 0, len(hpa.Spec.Metrics))

	for _, metric := range hpa.Spec.Metrics {
		typeName := MetricTypeName{
			Type: metric.Type,
		}

		typeName.Metric = metric.External.Metric

		config := &MetricConfig{
			MetricTypeName: typeName,
			Config:         map[string]string{},
			MetricSpec:     metric,
		}

		if metric.Type == autoscalingv2.ExternalMetricSourceType &&
			metric.External.Metric.Selector != nil &&
			metric.External.Metric.Selector.MatchLabels != nil {
			for k, v := range metric.External.Metric.Selector.MatchLabels {
				config.Config[k] = v
			}
		}
		if hpa.Annotations != nil {
			for k, v := range hpa.Annotations {
				config.Config[k] = v
			}
		}

		metricConfigs = append(metricConfigs, config)
	}
	return metricConfigs, nil
}
