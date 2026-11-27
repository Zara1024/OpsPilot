{{- define "opspilot.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "opspilot.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := include "opspilot.name" . -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "opspilot.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | quote }}
app.kubernetes.io/name: {{ include "opspilot.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "opspilot.selectorLabels" -}}
app.kubernetes.io/name: {{ include "opspilot.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "opspilot.managerName" -}}
{{- printf "%s-manager" (include "opspilot.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "opspilot.webName" -}}
{{- printf "%s-web" (include "opspilot.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "opspilot.frontierName" -}}
{{- printf "%s-frontier" (include "opspilot.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "opspilot.frontierEdgeServiceName" -}}
{{- printf "%s-frontier-edge" (include "opspilot.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "opspilot.serviceAccountName" -}}
{{- default (include "opspilot.managerName" .) .Values.manager.serviceAccountName -}}
{{- end -}}

{{- define "opspilot.secretName" -}}
{{- printf "%s-secrets" (include "opspilot.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "opspilot.pvcName" -}}
{{- printf "%s-data" (include "opspilot.managerName" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "opspilot.managerImage" -}}
{{- $repo := default "docker.cnb.cool/zara1024/opspilot" .Values.manager.image.repository -}}
{{- printf "%s:%s" $repo (default .Chart.AppVersion .Values.manager.image.tag) -}}
{{- end -}}

{{- define "opspilot.webImage" -}}
{{- $repo := default "docker.cnb.cool/zara1024/opspilot/opspilot-web" .Values.web.image.repository -}}
{{- printf "%s:%s" $repo (default .Chart.AppVersion .Values.web.image.tag) -}}
{{- end -}}

{{- define "opspilot.frontierImage" -}}
{{- $repo := default "docker.cnb.cool/zara1024/opspilot/frontier" .Values.frontier.image.repository -}}
{{- printf "%s:%s" $repo (default "1.2.6" .Values.frontier.image.tag) -}}
{{- end -}}

{{- define "opspilot.durationSeconds" -}}
{{- $raw := toString . -}}
{{- if regexMatch "^[1-9][0-9]*s$" $raw -}}
{{- trimSuffix "s" $raw -}}
{{- else if regexMatch "^[1-9][0-9]*m$" $raw -}}
{{- mul (int (trimSuffix "m" $raw)) 60 -}}
{{- else if regexMatch "^[1-9][0-9]*h$" $raw -}}
{{- mul (int (trimSuffix "h" $raw)) 3600 -}}
{{- else -}}
{{- fail (printf "HA duration must be a positive whole s/m/h string, got %q" $raw) -}}
{{- end -}}
{{- end -}}

{{- define "opspilot.hasInlineSecret" -}}
{{- $needDB := and (eq (default "mysql" .Values.database.dialect) "mysql") (empty .Values.database.existingSecret) (not (empty .Values.database.dsn)) -}}
{{- $needApp := and (empty .Values.secrets.existingSecret) (or (not (empty .Values.secrets.jwtSecret)) (not (empty .Values.secrets.secretKey)) (not (empty .Values.secrets.packetParserTokenSecret)) (not (empty .Values.secrets.adminPassword))) -}}
{{- if or $needDB $needApp -}}true{{- else -}}false{{- end -}}
{{- end -}}

{{- define "opspilot.validate" -}}
{{- $arch := default "" .Values.image.architecture -}}
{{- if and $arch (ne $arch "amd64") (ne $arch "arm64") -}}
{{- fail (printf "image.architecture must be empty, amd64, or arm64, got %q" $arch) -}}
{{- end -}}
{{- $dialect := default "mysql" .Values.database.dialect -}}
{{- if and (ne $dialect "mysql") (ne $dialect "sqlite") -}}
{{- fail (printf "database.dialect must be mysql or sqlite, got %q" $dialect) -}}
{{- end -}}
{{- $replicas := int .Values.manager.replicaCount -}}
{{- if and (eq $dialect "sqlite") (or .Values.manager.autoscaling.enabled (gt $replicas 1)) -}}
{{- fail "database.dialect=sqlite is not supported for multi-replica HA; configure external MySQL" -}}
{{- end -}}
{{- if and (eq $dialect "mysql") (empty .Values.database.dsn) (empty .Values.database.existingSecret) -}}
{{- fail "database.dsn or database.existingSecret is required when database.dialect=mysql" -}}
{{- end -}}
{{- if and (empty .Values.secrets.jwtSecret) (empty .Values.secrets.existingSecret) -}}
{{- fail "secrets.jwtSecret or secrets.existingSecret is required" -}}
{{- end -}}
{{- $ttl := int (include "opspilot.durationSeconds" .Values.manager.ha.leaseTTL) -}}
{{- $renew := int (include "opspilot.durationSeconds" .Values.manager.ha.leaseRenewInterval) -}}
{{- $retry := int (include "opspilot.durationSeconds" .Values.manager.ha.leaseRetryInterval) -}}
{{- if or (le $ttl 0) (le $renew 0) (ge $renew $ttl) (le $retry 0) -}}
{{- fail "manager.ha requires 0 < leaseRenewInterval < leaseTTL and leaseRetryInterval > 0" -}}
{{- end -}}
{{- if .Values.manager.autoscaling.enabled -}}
{{- $minR := int .Values.manager.autoscaling.minReplicas -}}
{{- $maxR := int .Values.manager.autoscaling.maxReplicas -}}
{{- if or (lt $minR 2) (lt $maxR $minR) -}}
{{- fail "manager.autoscaling requires 2 <= minReplicas <= maxReplicas" -}}
{{- end -}}
{{- else if .Values.manager.podDisruptionBudget.enabled -}}
{{- if ge (int .Values.manager.podDisruptionBudget.minAvailable) $replicas -}}
{{- fail "manager.podDisruptionBudget.minAvailable must be less than manager.replicaCount" -}}
{{- end -}}
{{- end -}}
{{- if and .Values.web.enabled .Values.web.podDisruptionBudget.enabled -}}
{{- if ge (int .Values.web.podDisruptionBudget.minAvailable) (int .Values.web.replicaCount) -}}
{{- fail "web.podDisruptionBudget.minAvailable must be less than web.replicaCount" -}}
{{- end -}}
{{- end -}}
{{- end -}}
