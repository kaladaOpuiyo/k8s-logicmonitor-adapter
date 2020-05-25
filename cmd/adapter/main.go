package main

import (
	"flag"
	"os"
	"runtime"

	"github.com/kaladaOpuiyo/k8s-logicmonitor-adapter/cmd"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/component-base/logs"
)

func main() {
	logs.InitLogs()
	defer logs.FlushLogs()

	if len(os.Getenv("GOMAXPROCS")) == 0 {
		runtime.GOMAXPROCS(runtime.NumCPU())
	}

	cmdRoot := cmd.NewAdapterServer(wait.NeverStop)
	cmdRoot.Flags().AddGoFlagSet(flag.CommandLine)
	if err := cmdRoot.Execute(); err != nil {
		panic(err)
	}
}
