package collector

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	c "github.com/kaladaOpuiyo/k8s-logicmonitor-adapter/pkg/collector"
	"github.com/logicmonitor/lm-sdk-go/client"
	logmon "github.com/logicmonitor/lm-sdk-go/client/lm"
	"k8s.io/api/autoscaling/v2beta2"
	autoscalingv2 "k8s.io/api/autoscaling/v2beta2"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/metrics/pkg/apis/external_metrics"
)

const (
	// LogicmonitorMetric ...
	LogicmonitorMetric = "logicmonitor"

	aggregator           = "aggregator"
	datapointName        = "datapoint"
	datapointPeriod      = "period"
	deviceDataSourceID   = "datasource"
	deviceDataSourceName = "datasource-name"
	deviceGroupID        = "devicegroup"
	deviceGroupName      = "device-group-name"
	deviceID             = "device"
	deviceName           = "device-name"
)

// LogicmonitorPlugin ...
type LogicmonitorPlugin struct {
	client *client.LMSdkGo
}

//NewLogicmonitorPlugin ...
func NewLogicmonitorPlugin(client *client.LMSdkGo) (*LogicmonitorPlugin, error) {
	return &LogicmonitorPlugin{
		client: client,
	}, nil
}

// NewCollector initializes a new logicmonitor collector from the specified HPA..
func (lm *LogicmonitorPlugin) NewCollector(hpa *v2beta2.HorizontalPodAutoscaler, config *c.MetricConfig, interval time.Duration) (c.Collector, error) {

	switch config.Metric.Name {
	case LogicmonitorMetric:
		return NewLogicmonitorCollector(lm.client, config, interval)
	}

	return nil, fmt.Errorf("metric '%s' not supported", config.Metric.Name)
}

//LogicmonitorCollector ...
type LogicmonitorCollector struct {
	aggregator           c.AggregatorFunc
	client               *client.LMSdkGo
	DataPointName        string
	DeviceDataSourceID   string
	DeviceDataSourceName string
	DeviceGroupID        string
	DeviceGroupName      string
	DeviceID             string
	DeviceName           string
	Period               float64

	interval   time.Duration
	metric     autoscalingv2.MetricIdentifier
	metricType autoscalingv2.MetricSourceType
}

//NewLogicmonitorCollector ...
func NewLogicmonitorCollector(client *client.LMSdkGo, config *c.MetricConfig, interval time.Duration) (*LogicmonitorCollector, error) {

	deviceName, ok := config.Config[deviceName]
	if !ok {
		deviceName = ""
	}

	deviceID, ok := config.Config[deviceID]
	if !ok {
		deviceID = ""
	}

	deviceGroupName, ok := config.Config[deviceGroupName]
	if !ok {
		deviceGroupName = ""
	}

	deviceGroupID, ok := config.Config[deviceGroupID]
	if !ok {
		deviceGroupID = ""
	}

	deviceDataSourceName, ok := config.Config[deviceDataSourceName]
	if !ok {
		deviceDataSourceName = ""
	}

	deviceDataSourceID, ok := config.Config[deviceDataSourceID]
	if !ok {
		deviceDataSourceID = ""
	}

	datapoint, ok := config.Config[datapointName]
	if !ok {
		return nil, fmt.Errorf("Datapoint Name not found in configuration")
	}

	agg, ok := config.Config[aggregator]
	if !ok {
		agg = "avg"
	}

	p, ok := config.Config[datapointPeriod]
	if !ok {
		p = "10"
	}

	period, err := strconv.ParseFloat(p, 64)
	if err != nil {
		return nil, fmt.Errorf("Datapoint Name not found in configuration: %s", err)
	}

	aggFunc, err := c.ParseAggregator(agg)
	if err != nil {
		return nil, fmt.Errorf("aggregator not found in configuration: %s", err)
	}

	return &LogicmonitorCollector{
		aggregator:           aggFunc,
		client:               client,
		DataPointName:        datapoint,
		DeviceDataSourceName: deviceDataSourceName,
		DeviceDataSourceID:   deviceDataSourceID,
		DeviceGroupName:      deviceGroupName,
		DeviceName:           deviceName,
		DeviceGroupID:        deviceGroupID,
		DeviceID:             deviceID,
		interval:             interval,
		metric:               config.Metric,
		metricType:           config.Type,
		Period:               period / 60,
	}, nil
}

func (lm *LogicmonitorCollector) getMetricsDeviceGroupMetrics(client *client.LMSdkGo, name, id string) ([]float64, error) {

	var (
		deviceGroupID int32
		devicesIDs    []int32
		metrics       []float64
	)

	if validID, err := strconv.ParseInt(id, 10, 32); err == nil {
		deviceGroupID = int32(validID)

	} else {

		deviceGroupID, err = lm.getDeviceGroupDeviceID(client, name)
		if err != nil {
			return nil, err
		}
	}

	devicesIDs, err := lm.getDeviceGroupDeviceIDs(client, deviceGroupID)
	if err != nil {
		return nil, err
	}

	for _, deviceID := range devicesIDs {
		m, err := lm.getMetrics(client, deviceID)
		if err != nil {
			return nil, err
		}
		metrics = append(metrics, m...)
	}

	return metrics, nil

}

func (lm *LogicmonitorCollector) getMetricsDeviceMetrics(client *client.LMSdkGo, deviceName, id string) ([]float64, error) {

	var (
		deviceID int32
		err      error
		metrics  []float64
	)

	if validID, err := strconv.ParseInt(id, 10, 32); err == nil {
		deviceID = int32(validID)
	} else {

		deviceID, err = lm.getDeviceID(client, deviceName)
		if err != nil {
			return nil, err
		}
	}

	metrics, err = lm.getMetrics(client, deviceID)
	if err != nil {
		return nil, err
	}

	return metrics, nil

}

//GetMetrics ...
func (lm *LogicmonitorCollector) GetMetrics() ([]c.CollectedMetric, error) {

	var (
		metrics []float64
		err     error
	)

	if lm.DeviceGroupName != "" || lm.DeviceGroupID != "" {
		metrics, err = lm.getMetricsDeviceGroupMetrics(lm.client, lm.DeviceGroupName, lm.DeviceGroupID)
		if err != nil {
			return nil, err
		}
	} else if lm.DeviceName != "" || lm.DeviceID != "" {
		metrics, err = lm.getMetricsDeviceMetrics(lm.client, lm.DeviceName, lm.DeviceID)
		if err != nil {
			return nil, err
		}

	}

	metric := lm.aggregator(metrics...)

	metricValue := c.CollectedMetric{
		Type: lm.metricType,
		External: external_metrics.ExternalMetricValue{
			MetricName:   lm.metric.Name,
			MetricLabels: lm.metric.Selector.MatchLabels,
			Timestamp:    metav1.Time{Time: time.Now().UTC()},
			Value:        *resource.NewMilliQuantity(int64(metric)*1000, resource.DecimalSI),
		},
	}

	return []c.CollectedMetric{metricValue}, nil
}

// Interval returns the interval at which the collector should run.
func (lm *LogicmonitorCollector) Interval() time.Duration {
	return lm.interval
}

//GetMetrics ...
func (lm *LogicmonitorCollector) getMetrics(client *client.LMSdkGo, deviceID int32) ([]float64, error) {

	var (
		datapointValues []float64
		hdsID           int32
	)

	if validID, err := strconv.ParseInt(lm.DeviceDataSourceID, 10, 32); err == nil {
		hdsID = int32(validID)
	} else {
		hdsID, err = lm.getDataSourceID(client, lm.DeviceDataSourceName, deviceID)
		if err != nil {
			return nil, err
		}
	}

	instancesIDs, err := lm.getDeviceDatasourceInstanceIDs(client, deviceID, hdsID)
	if err != nil {
		return nil, err
	}
	for _, instancesID := range instancesIDs {
		params := logmon.NewGetDeviceDatasourceInstanceDataParams()
		params.DeviceID = deviceID
		params.HdsID = hdsID
		params.ID = instancesID
		params.Datapoints = &lm.DataPointName
		params.Period = &lm.Period

		// get device instance data
		resp, err := client.LM.GetDeviceDatasourceInstanceData(params)
		if err != nil {
			return nil, err
		}

		for _, values := range resp.Payload.Values {
			for _, newDatapointValue := range values {

				if newDatapointValue != "No Data" {

					v, err := newDatapointValue.(json.Number).Float64()
					if err != nil {
						return nil, err
					}
					datapointValues = append(datapointValues, v)
				}
			}

		}
	}

	return datapointValues, nil
}

// FindByDisplayName implements types.DeviceManager.
func (lm *LogicmonitorCollector) getDeviceID(client *client.LMSdkGo, name string) (int32, error) {

	filter := fmt.Sprintf("displayName:\"%s\"", name)
	params := logmon.NewGetDeviceListParams()
	fields := "id"
	params.SetFields(&fields)
	params.SetFilter(&filter)

	// get device list
	resp, err := client.LM.GetDeviceList(params)
	if err != nil {
		return 0, err
	}
	if resp.Payload.Total == 1 {
		return resp.Payload.Items[0].ID, nil
	}

	return 0, nil
}

func (lm *LogicmonitorCollector) getDataSourceID(client *client.LMSdkGo, name string, deviceID int32) (int32, error) {

	params := logmon.NewGetDeviceDatasourceListParams()
	filter := fmt.Sprintf("instanceNumber>0,dataSourceName~\"%s\"", name)
	params.DeviceID = deviceID
	fields := "id"
	params.SetFields(&fields)
	params.SetFilter(&filter)

	// get device datasource list
	resp, err := client.LM.GetDeviceDatasourceList(params)
	if err != nil {
		return 0, err
	}

	if resp.Payload.Total == 1 {
		return resp.Payload.Items[0].ID, nil
	}

	return 0, nil

}

func (lm *LogicmonitorCollector) getDeviceDatasourceInstanceIDs(client *client.LMSdkGo, deviceID, hdsID int32) ([]int32, error) {

	var instanceIDs []int32

	params := logmon.NewGetDeviceDatasourceInstanceListParams()
	params.DeviceID = deviceID
	params.HdsID = hdsID

	// get device instance list
	resp, err := client.LM.GetDeviceDatasourceInstanceList(params)
	if err != nil {
		return nil, fmt.Errorf("device id:%d,datasource id:%d gave error message: %s", deviceID, hdsID, err)
	}

	for _, instance := range resp.Payload.Items {
		instanceIDs = append(instanceIDs, instance.ID)
	}

	return instanceIDs, nil
}

func (lm *LogicmonitorCollector) getDeviceGroupDeviceIDs(client *client.LMSdkGo, deviceGroup int32) ([]int32, error) {

	var deviceIDs []int32

	params := logmon.NewGetImmediateDeviceListByDeviceGroupIDParams()
	params.ID = deviceGroup

	resp, err := client.LM.GetImmediateDeviceListByDeviceGroupID(params)
	if err != nil {
		return nil, err
	}

	for _, device := range resp.Payload.Items {
		deviceIDs = append(deviceIDs, device.ID)
	}

	return deviceIDs, nil
}

func (lm *LogicmonitorCollector) getDeviceGroupDeviceID(client *client.LMSdkGo, name string) (int32, error) {

	params := logmon.NewGetDeviceGroupListParams()
	fields := "id"
	params.SetFields(&fields)
	filter := fmt.Sprintf("name\"%s\"", name)
	params.SetFilter(&filter)

	resp, err := client.LM.GetDeviceGroupList(params)
	if err != nil {
		return 0, err
	}

	if resp.Payload.Total == 1 {
		return resp.Payload.Items[0].ID, nil
	}

	return 0, nil
}
