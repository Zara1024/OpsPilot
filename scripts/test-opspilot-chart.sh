#!/usr/bin/env bash
set -euo pipefail

chart_dir=${1:-deploy/kubernetes/opspilot}
chart_package=${2:-bin/k8s/opspilot.tgz}
expected_manager_image=${3:-}
expected_web_image=${4:-}

tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT

common_args=(
  --namespace opspilot-system
  --set-string 'database.dsn=opspilot:secret@tcp(mysql.infra.svc:3306)/opspilot?parseTime=true&charset=utf8mb4&loc=UTC'
  --set-string 'secrets.jwtSecret=test-jwt-secret-must-be-long-enough-for-prod'
)

extract_source() {
  local source=$1
  local input=$2
  local output=$3
  awk -v marker="# Source: ${source}" '
    $0 == marker { capture = 1; next }
    capture && /^---$/ { exit }
    capture { print }
  ' "$input" >"$output"
}

expect_template_failure() {
  local expected=$1
  shift
  if "$@" >"$tmp_dir/failure.log" 2>&1; then
    echo "expected Helm template failure containing: $expected" >&2
    exit 1
  fi
  grep -Fq "$expected" "$tmp_dir/failure.log" || {
    echo "failure output did not contain '$expected':" >&2
    cat "$tmp_dir/failure.log" >&2
    exit 1
  }
}

# 1. Lint & render default 2-replica HA configuration.
helm lint "$chart_dir" "${common_args[@]}"

helm template opspilot "$chart_package" "${common_args[@]}" >"$tmp_dir/default.yaml"
extract_source 'opspilot/templates/manager-deployment.yaml' "$tmp_dir/default.yaml" "$tmp_dir/manager-dep.yaml"
extract_source 'opspilot/templates/web-deployment.yaml' "$tmp_dir/default.yaml" "$tmp_dir/web-dep.yaml"
extract_source 'opspilot/templates/web-configmap.yaml' "$tmp_dir/default.yaml" "$tmp_dir/web-cm.yaml"
extract_source 'opspilot/templates/frontier-deployment.yaml' "$tmp_dir/default.yaml" "$tmp_dir/frontier-dep.yaml"
extract_source 'opspilot/templates/frontier-configmap.yaml' "$tmp_dir/default.yaml" "$tmp_dir/frontier-cm.yaml"
extract_source 'opspilot/templates/manager-policy.yaml' "$tmp_dir/default.yaml" "$tmp_dir/manager-policy.yaml"
extract_source 'opspilot/templates/web-pdb.yaml' "$tmp_dir/default.yaml" "$tmp_dir/web-pdb.yaml"
extract_source 'opspilot/templates/secret.yaml' "$tmp_dir/default.yaml" "$tmp_dir/secret.yaml"

grep -q 'replicas: 2' "$tmp_dir/manager-dep.yaml"
grep -q 'replicas: 2' "$tmp_dir/web-dep.yaml"
grep -q 'type: RollingUpdate' "$tmp_dir/manager-dep.yaml"
grep -q 'maxUnavailable: 0' "$tmp_dir/manager-dep.yaml"
grep -q 'maxSurge: 1' "$tmp_dir/manager-dep.yaml"
grep -q 'fieldPath: metadata.name' "$tmp_dir/manager-dep.yaml"
grep -q 'service.instance.id=$(POD_UID)' "$tmp_dir/manager-dep.yaml"
grep -q 'path: /readyz' "$tmp_dir/manager-dep.yaml"
grep -q 'path: /healthz' "$tmp_dir/manager-dep.yaml"
grep -q 'preStop:' "$tmp_dir/manager-dep.yaml"
grep -q 'terminationGracePeriodSeconds: 45' "$tmp_dir/manager-dep.yaml"
grep -q 'runAsNonRoot: true' "$tmp_dir/manager-dep.yaml"
grep -q 'runAsUser: 65532' "$tmp_dir/manager-dep.yaml"
grep -q 'topologySpreadConstraints:' "$tmp_dir/manager-dep.yaml"
grep -q 'kind: PodDisruptionBudget' "$tmp_dir/manager-policy.yaml"
grep -q 'minAvailable: 1' "$tmp_dir/manager-policy.yaml"
grep -q 'kind: PodDisruptionBudget' "$tmp_dir/web-pdb.yaml"
grep -q 'runAsUser: 101' "$tmp_dir/web-dep.yaml"
grep -q 'listen 8080;' "$tmp_dir/web-cm.yaml"
grep -q 'pid /tmp/nginx.pid;' "$tmp_dir/web-cm.yaml"
! grep -q 'myqcloud.com' "$tmp_dir/web-cm.yaml"
grep -q 'edgeid_alloc_when_no_idservice_on: false' "$tmp_dir/frontier-cm.yaml"
grep -q 'image: "docker.cnb.cool/zara1024/opspilot/frontier:1.2.6"' "$tmp_dir/frontier-dep.yaml"
grep -q 'kind: Secret' "$tmp_dir/secret.yaml"
if [[ -n "$expected_manager_image" ]]; then
  grep -Fq "image: \"${expected_manager_image}\"" "$tmp_dir/manager-dep.yaml"
fi
if [[ -n "$expected_web_image" ]]; then
  grep -Fq "image: \"${expected_web_image}\"" "$tmp_dir/web-dep.yaml"
fi

# 2. Autoscaling omits static Deployment replicas and renders HPA.
helm template opspilot "$chart_package" "${common_args[@]}" \
  --set manager.autoscaling.enabled=true >"$tmp_dir/hpa.yaml"
extract_source 'opspilot/templates/manager-deployment.yaml' "$tmp_dir/hpa.yaml" "$tmp_dir/hpa-manager.yaml"
! grep -q '^  replicas:' "$tmp_dir/hpa-manager.yaml"
grep -q 'kind: HorizontalPodAutoscaler' "$tmp_dir/hpa.yaml"

# 3. External secrets mode must not emit inline Secret and must bind external secretKeyRef.
helm template opspilot "$chart_package" \
  --namespace opspilot-system \
  --set database.existingSecret=ext-db \
  --set secrets.existingSecret=ext-app >"$tmp_dir/ext-secrets.yaml"
! grep -q '# Source: opspilot/templates/secret.yaml' "$tmp_dir/ext-secrets.yaml"
grep -q 'name: "ext-db"' "$tmp_dir/ext-secrets.yaml"
grep -q 'name: "ext-app"' "$tmp_dir/ext-secrets.yaml"

# 4. Persistence + Ingress + ServiceMonitor + PrometheusRule rendering.
helm template opspilot "$chart_package" "${common_args[@]}" \
  --set manager.persistence.enabled=true \
  --set ingress.enabled=true \
  --set 'ingress.hosts[0].host=ops.example.com' \
  --set metrics.serviceMonitor.enabled=true \
  --set metrics.prometheusRule.enabled=true >"$tmp_dir/full.yaml"
grep -q 'kind: PersistentVolumeClaim' "$tmp_dir/full.yaml"
grep -q 'ReadWriteMany' "$tmp_dir/full.yaml"
grep -q 'kind: Ingress' "$tmp_dir/full.yaml"
grep -q 'kind: ServiceMonitor' "$tmp_dir/full.yaml"
grep -q 'kind: PrometheusRule' "$tmp_dir/full.yaml"
grep -q 'OpsPilotClusterLeaseNoLeader' "$tmp_dir/full.yaml"
grep -q 'OpsPilotClusterLeaseSplitBrain' "$tmp_dir/full.yaml"

# 5. Negative safety validations.
expect_template_failure 'sqlite is not supported' \
  helm template invalid-sqlite "$chart_package" "${common_args[@]}" --set database.dialect=sqlite

expect_template_failure 'database.dsn or database.existingSecret is required' \
  helm template invalid-no-db "$chart_package" --namespace opspilot-system --set secrets.jwtSecret=abc

expect_template_failure 'secrets.jwtSecret or secrets.existingSecret is required' \
  helm template invalid-no-jwt "$chart_package" --namespace opspilot-system --set database.dsn=dsn

expect_template_failure '0 < leaseRenewInterval < leaseTTL' \
  helm template invalid-lease "$chart_package" "${common_args[@]}" --set manager.ha.leaseRenewInterval=20s

expect_template_failure 'minAvailable must be less than manager.replicaCount' \
  helm template invalid-pdb "$chart_package" "${common_args[@]}" --set manager.podDisruptionBudget.minAvailable=2

expect_template_failure 'image.architecture must be empty, amd64, or arm64' \
  helm template invalid-arch "$chart_package" "${common_args[@]}" --set image.architecture=s390x

printf 'control-plane HA Helm chart verified\n'
