package client

import (
	cfg "github.com/kaladaOpuiyo/k8s-logicmonitor-adapter/pkg/config"
	"github.com/logicmonitor/lm-sdk-go/client"
)

// NewLogicmonitorClient creates a Logicmonitor client.
func NewLogicmonitorClient(secrets *cfg.Config) *client.LMSdkGo {

	config := client.NewConfig()
	accessID := secrets.ID
	config.SetAccessID(&accessID)
	accessKey := secrets.Key
	config.SetAccessKey(&accessKey)
	domain := secrets.Account + ".logicmonitor.com"
	config.SetAccountDomain(&domain)

	return client.New(config)
}
