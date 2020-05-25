package cmd

import (
	"context"
	"fmt"
	"strconv"
	"time"

	lmclient "github.com/kaladaOpuiyo/k8s-logicmonitor-adapter/pkg/client"
	"github.com/kaladaOpuiyo/k8s-logicmonitor-adapter/pkg/collector"
	cfg "github.com/kaladaOpuiyo/k8s-logicmonitor-adapter/pkg/config"
	lm "github.com/kaladaOpuiyo/k8s-logicmonitor-adapter/pkg/logicmonitor"
	hpa "github.com/kaladaOpuiyo/k8s-logicmonitor-adapter/pkg/metrics-adapter/hpa"
	"github.com/kubernetes-incubator/custom-metrics-apiserver/pkg/cmd/server"
	"github.com/spf13/cobra"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// AdapterServerOptions ...
type AdapterServerOptions struct {
	*server.CustomMetricsAdapterServerOptions

	// RemoteKubeConfigFile is the config used to list pods from the master API server
	RemoteKubeConfigFile string

	// Collection Interval
	CollectionInterval string
}

// NewAdapterServer provides a CLI handler for 'start adapter server' command
func NewAdapterServer(stopCh <-chan struct{}) *cobra.Command {
	baseOpts := server.NewCustomMetricsAdapterServerOptions()
	o := AdapterServerOptions{
		CustomMetricsAdapterServerOptions: baseOpts,
	}

	cmd := &cobra.Command{
		Short: "Logicmonitor metrics API adapter server",
		Long:  "Logicmonitor metrics API adapter server",
		RunE: func(c *cobra.Command, args []string) error {
			if err := o.Complete(); err != nil {
				return err
			}
			if err := o.Validate(args); err != nil {
				return err
			}
			if err := o.RunAdapterServer(stopCh); err != nil {
				return err
			}
			return nil
		},
	}

	flags := cmd.Flags()
	o.SecureServing.AddFlags(flags)
	o.Authentication.AddFlags(flags)
	o.Authorization.AddFlags(flags)
	o.Features.AddFlags(flags)

	flags.StringVar(&o.RemoteKubeConfigFile, "kubeconfig", o.RemoteKubeConfigFile, ""+
		"kubeconfig file pointing at the 'core' kubernetes server with enough rights to list "+
		"any described objects")
	flags.StringVar(&o.CollectionInterval, "collection-interval", o.CollectionInterval, ""+
		"set a metrics collection interval in mins")
	return cmd
}

// RunAdapterServer ...
func (o AdapterServerOptions) RunAdapterServer(stopCh <-chan struct{}) error {

	config, err := o.Config()
	if err != nil {
		return err
	}

	var clientConfig *rest.Config
	if len(o.RemoteKubeConfigFile) > 0 {
		loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: o.RemoteKubeConfigFile}
		loader := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, &clientcmd.ConfigOverrides{})

		clientConfig, err = loader.ClientConfig()
	} else {
		clientConfig, err = rest.InClusterConfig()
	}
	if err != nil {
		return fmt.Errorf("unable to construct lister client config to initialize metrics-adapter: %v", err)
	}

	// convert stop channel to a context
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-stopCh
		cancel()
	}()

	clientConfig.Timeout = 30 * time.Second

	client, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		return fmt.Errorf("failed to initialize new client: %v", err)
	}

	secrets, err := cfg.GetConfig()
	if err != nil {
		return fmt.Errorf("Failed to get secrets: %s", err)
	}

	collectorFactory := collector.NewCollectorFactory()

	logmonitorPlugin, err := lm.NewLogicmonitorPlugin(lmclient.NewLogicmonitorClient(secrets))
	if err != nil {
		return fmt.Errorf("failed to initialize logicmonitor collector plugin: %v", err)
	}
	collectorFactory.RegisterExternalCollector([]string{lm.LogicmonitorMetric}, logmonitorPlugin)

	var ci int
	ci, err = strconv.Atoi(o.CollectionInterval)
	if err != nil {
		ci = 5
	}
	collectionInterval := time.Duration(ci) * time.Minute

	hpaProvider := hpa.NewHPAProvider(client, 30*time.Second, collectionInterval, collectorFactory)

	go hpaProvider.Run(ctx)

	externalMetricsProvider := hpaProvider

	informer := informers.NewSharedInformerFactory(client, 0)

	server, err := config.Complete(informer).New("k8s-logicmonitor-adapter", nil, externalMetricsProvider)
	if err != nil {
		return err
	}
	return server.GenericAPIServer.PrepareRun().Run(ctx.Done())
}
