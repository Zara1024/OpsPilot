{{- define "opspilot-edge.name" -}}
{{- $architecture := default "" .Values.image.architecture -}}
{{- if and $architecture (ne $architecture "amd64") (ne $architecture "arm64") -}}
{{- fail "image.architecture must be empty, amd64, or arm64" -}}
{{- end -}}
{{- .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "opspilot-edge.fullname" -}}
{{- printf "%s" (include "opspilot-edge.name" .) -}}
{{- end -}}

{{- define "opspilot-edge.labels" -}}
app.kubernetes.io/name: {{ include "opspilot-edge.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
opspilot.io/k8s-mode: {{ .Values.mode | quote }}
opspilot.io/cluster-id: {{ .Values.enrollment.clusterID | quote }}
{{- end -}}

{{- define "opspilot-edge.nodeServiceAccount" -}}
{{- default (printf "%s-node" (include "opspilot-edge.fullname" .)) .Values.node.serviceAccountName -}}
{{- end -}}

{{- define "opspilot-edge.controllerServiceAccount" -}}
{{- default (printf "%s-controller" (include "opspilot-edge.fullname" .)) .Values.controller.serviceAccountName -}}
{{- end -}}

{{- define "opspilot-edge.telemetryGatewayServiceAccount" -}}
{{- $gw := default dict .Values.telemetryGateway -}}
{{- default (printf "%s-telemetry-gateway" (include "opspilot-edge.fullname" .)) $gw.serviceAccountName -}}
{{- end -}}

{{- define "opspilot-edge.metricsScraperServiceAccount" -}}
{{- $metrics := default dict .Values.kubernetesMetrics -}}
{{- default (printf "%s-metrics-scraper" (include "opspilot-edge.fullname" .)) $metrics.serviceAccountName -}}
{{- end -}}

{{- define "opspilot-edge.kubeStateMetricsName" -}}
{{- printf "%s-kube-state-metrics" (include "opspilot-edge.fullname" .) -}}
{{- end -}}

{{- define "opspilot-edge.telemetryGatewayName" -}}
{{- printf "%s-telemetry-gateway" (include "opspilot-edge.fullname" .) -}}
{{- end -}}

{{- define "opspilot-edge.telemetryBackendLabel" -}}
opspilot.io/telemetry-backend
{{- end -}}

{{- define "opspilot-edge.upgradeHookServiceAccount" -}}
{{- printf "%s-upgrade-preflight" (include "opspilot-edge.fullname" .) -}}
{{- end -}}

{{- define "opspilot-edge.controllerCredentialSecretName" -}}
{{- printf "%s-controller-credentials" (include "opspilot-edge.fullname" .) -}}
{{- end -}}

{{- define "opspilot-edge.telemetryCredentialSecretName" -}}
{{- printf "%s-telemetry-credentials" (include "opspilot-edge.fullname" .) -}}
{{- end -}}

{{- define "opspilot-edge.telemetryGatewayMode" -}}
{{- $gw := default dict .Values.telemetryGateway -}}
{{- $mode := default "deployment" $gw.mode -}}
{{- if and (ne $mode "embedded") (ne $mode "deployment") -}}
{{- fail "telemetryGateway.mode must be embedded or deployment" -}}
{{- end -}}
{{- $mode -}}
{{- end -}}

{{- define "opspilot-edge.kubernetesMetricsMode" -}}
{{- $metrics := default dict .Values.kubernetesMetrics -}}
{{- $mode := default "scraper" $metrics.mode -}}
{{- if and (ne $mode "controller") (ne $mode "scraper") -}}
{{- fail "kubernetesMetrics.mode must be controller or scraper" -}}
{{- end -}}
{{- $mode -}}
{{- end -}}

{{- define "opspilot-edge.kubernetesMetricsEnabled" -}}
{{- $metrics := default dict .Values.kubernetesMetrics -}}
{{- $controllerMetrics := default dict .Values.controller.metrics -}}
{{- if kindIs "bool" $metrics.enabled -}}
{{- if $metrics.enabled -}}true{{- else -}}false{{- end -}}
{{- else if or $metrics.endpoint (default false $controllerMetrics.enabled) (eq (include "opspilot-edge.kubeStateMetricsEnabled" .) "true") (eq (include "opspilot-edge.kubernetesAppMetricsDiscoveryEnabled" .) "true") -}}
true
{{- else -}}
false
{{- end -}}
{{- end -}}

{{- define "opspilot-edge.kubernetesAppMetricsDiscoveryEnabled" -}}
{{- $metrics := default dict .Values.kubernetesMetrics -}}
{{- $app := default dict $metrics.appDiscovery -}}
{{- $controllerMetrics := default dict .Values.controller.metrics -}}
{{- $legacyApp := default dict $controllerMetrics.appDiscovery -}}
{{- if kindIs "bool" $app.enabled -}}
{{- if $app.enabled -}}true{{- else -}}false{{- end -}}
{{- else if kindIs "bool" $legacyApp.enabled -}}
{{- if $legacyApp.enabled -}}true{{- else -}}false{{- end -}}
{{- else -}}
false
{{- end -}}
{{- end -}}

{{- define "opspilot-edge.memoryQuantityMiB" -}}
{{- $raw := toString . -}}
{{- if regexMatch "^[1-9][0-9]*Mi$" $raw -}}
{{- trimSuffix "Mi" $raw -}}
{{- else if regexMatch "^[1-9][0-9]*Gi$" $raw -}}
{{- mul (int (trimSuffix "Gi" $raw)) 1024 -}}
{{- else -}}
{{- fail (printf "telemetryGateway.resources.limits.memory must be a whole Mi or Gi quantity, got %q" $raw) -}}
{{- end -}}
{{- end -}}

{{- define "opspilot-edge.durationSeconds" -}}
{{- $raw := toString . -}}
{{- if regexMatch "^[1-9][0-9]*s$" $raw -}}
{{- trimSuffix "s" $raw -}}
{{- else if regexMatch "^[1-9][0-9]*m$" $raw -}}
{{- mul (int (trimSuffix "m" $raw)) 60 -}}
{{- else if regexMatch "^[1-9][0-9]*h$" $raw -}}
{{- mul (int (trimSuffix "h" $raw)) 3600 -}}
{{- else -}}
{{- fail (printf "upgrade.migrationHook.timeout must be a whole second, minute, or hour duration, got %q" $raw) -}}
{{- end -}}
{{- end -}}

{{- define "opspilot-edge.telemetryGatewayEnabled" -}}
{{- $gw := default dict .Values.telemetryGateway -}}
{{- if kindIs "bool" $gw.enabled -}}
{{- if $gw.enabled -}}true{{- else -}}false{{- end -}}
{{- else -}}
true
{{- end -}}
{{- end -}}

{{- define "opspilot-edge.upgradeMigrationHookEnabled" -}}
{{- $upgrade := default dict .Values.upgrade -}}
{{- $hook := default dict $upgrade.migrationHook -}}
{{- if kindIs "bool" $hook.enabled -}}
{{- if $hook.enabled -}}true{{- else -}}false{{- end -}}
{{- else -}}
true
{{- end -}}
{{- end -}}

{{- define "opspilot-edge.kubeStateMetricsEnabled" -}}
{{- $ksm := default dict .Values.kubeStateMetrics -}}
{{- if kindIs "bool" $ksm.enabled -}}
{{- if $ksm.enabled -}}true{{- else -}}false{{- end -}}
{{- else -}}
true
{{- end -}}
{{- end -}}

{{- define "opspilot-edge.kubeStateMetricsServiceAccount" -}}
{{- $ksm := default dict .Values.kubeStateMetrics -}}
{{- default (include "opspilot-edge.kubeStateMetricsName" .) $ksm.serviceAccountName -}}
{{- end -}}

{{- define "opspilot-edge.kubeStateMetricsEndpoint" -}}
{{- $ksm := default dict .Values.kubeStateMetrics -}}
{{- $port := default 8080 $ksm.port -}}
{{- printf "http://%s.%s.svc:%v/metrics" (include "opspilot-edge.kubeStateMetricsName" .) .Release.Namespace $port -}}
{{- end -}}

{{- define "opspilot-edge.k8sMetricsEndpoint" -}}
{{- $controllerMetrics := default dict .Values.controller.metrics -}}
{{- if $controllerMetrics.endpoint -}}
{{- $controllerMetrics.endpoint -}}
{{- else if eq (include "opspilot-edge.kubeStateMetricsEnabled" .) "true" -}}
{{- include "opspilot-edge.kubeStateMetricsEndpoint" . -}}
{{- end -}}
{{- end -}}

{{- define "opspilot-edge.kubernetesMetricsEndpoint" -}}
{{- $metrics := default dict .Values.kubernetesMetrics -}}
{{- $controllerMetrics := default dict .Values.controller.metrics -}}
{{- if $metrics.endpoint -}}
{{- $metrics.endpoint -}}
{{- else if $controllerMetrics.endpoint -}}
{{- $controllerMetrics.endpoint -}}
{{- else if eq (include "opspilot-edge.kubeStateMetricsEnabled" .) "true" -}}
{{- include "opspilot-edge.kubeStateMetricsEndpoint" . -}}
{{- end -}}
{{- end -}}

{{- define "opspilot-edge.k8sMetricsEnabled" -}}
{{- $controllerMetrics := default dict .Values.controller.metrics -}}
{{- if and (eq (include "opspilot-edge.kubernetesMetricsEnabled" .) "true") (eq (include "opspilot-edge.kubernetesMetricsMode" .) "controller") (or (default false $controllerMetrics.enabled) (eq (include "opspilot-edge.kubeStateMetricsEnabled" .) "true") (eq (include "opspilot-edge.kubernetesAppMetricsDiscoveryEnabled" .) "true")) -}}true{{- else -}}false{{- end -}}
{{- end -}}

{{- define "opspilot-edge.kubeStateMetricsResources" -}}
{{- $ksm := default dict .Values.kubeStateMetrics -}}
{{- if $ksm.collectors -}}
{{- join "," $ksm.collectors -}}
{{- else -}}
{{- "pods,deployments,statefulsets,daemonsets,replicasets,jobs,cronjobs,services,nodes,namespaces" -}}
{{- end -}}
{{- end -}}

{{- define "opspilot-edge.image" -}}
{{- $repo := default "docker.cnb.cool/zara1024/opspilot-edge" .Values.image.repository -}}
{{- printf "%s:%s" $repo (default .Chart.AppVersion .Values.image.tag) -}}
{{- end -}}
