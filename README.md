# k8s-Logicmonitor-Adapter
> An adapter to pull Logicmonitor metrics into kubernetes for horizontal pod scaling 
> Inspired by [kube-metrics-adapter](https://github.com/zalando-incubator/kube-metrics-adapter) 

## Install Example
``` shell
helm upgrade --install --debug --wait --namespace="operations" --set accessID=""  --set accessKey=""  --set account="" --set imageTag="0.0.1" --set collectorInvterval="1"  --set verboseLevel=1 --set imageRepository="urbanradikal/k8s-logicmonitor-adapter"  --version 1.0.0  k8-logicmonitor-adapter   k8s-logicmonitor-adapter/chart/k8s-logicmonitor-adapter
```