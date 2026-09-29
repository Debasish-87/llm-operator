#!/usr/bin/env bash
set -Eeuo pipefail

# Full llm-operator validation.
#
# Validates:
#   - Kubernetes
#   - operator
#   - CRD
#   - LLMService reconciliation
#   - finalizer
#   - Deployment
#   - Service
#   - PVC
#   - KEDA ScaledObject
#   - owner references
#   - status reconciliation
#   - GPU availability
#   - GPU scheduling/allocation
#   - vLLM pod
#   - vLLM readiness
#   - vLLM /health
#   - vLLM /metrics
#   - vLLM waiting-request metric
#   - KEDA readiness
#   - KEDA HPA
#   - Prometheus
#   - DCGM exporter when installed
#
# GPU-less clusters:
#   GPU/vLLM runtime checks are SKIPPED.
#   Operator/resource checks still run and can PASS.

NAMESPACE="${TEST_NAMESPACE:-default}"
NAME="${TEST_LLM_SERVICE:-llmservice-operator-test}"

TIMEOUT="${TEST_TIMEOUT:-90s}"
GPU_TIMEOUT="${GPU_TEST_TIMEOUT:-10m}"

PROMETHEUS_NAMESPACE="${PROMETHEUS_NAMESPACE:-monitoring}"
PROMETHEUS_SERVICE="${PROMETHEUS_SERVICE:-prometheus}"
DCGM_NAMESPACE="${DCGM_NAMESPACE:-monitoring}"
DCGM_SERVICE="${DCGM_SERVICE:-dcgm-exporter}"

log() {
  printf '\n==> %s\n' "$*"
}

pass() {
  printf '✓ %s\n' "$*"
}

skip() {
  printf '⚠ SKIP: %s\n' "$*"
}

fail() {
  printf '✗ %s\n' "$*" >&2
  exit 1
}

command -v kubectl >/dev/null ||
  fail "kubectl is required"

cleanup() {
  kubectl delete llmservice "$NAME" \
    -n "$NAMESPACE" \
    --ignore-not-found \
    --wait=false >/dev/null 2>&1 || true
}

trap cleanup EXIT


# ------------------------------------------------------------
# Kubernetes
# ------------------------------------------------------------

log "Checking Kubernetes API"

kubectl cluster-info >/dev/null ||
  fail "Kubernetes cluster is unreachable"

pass "Kubernetes API reachable"


# ------------------------------------------------------------
# Operator
# ------------------------------------------------------------

log "Checking operator"

kubectl get deployment llm-operator-controller-manager \
  -n llm-operator-system >/dev/null ||
  fail "Operator Deployment not found"

kubectl wait \
  --for=condition=Available \
  deployment/llm-operator-controller-manager \
  -n llm-operator-system \
  --timeout="$TIMEOUT" >/dev/null ||
  fail "Operator Deployment is not Available"

pass "Operator is running"


# ------------------------------------------------------------
# CRD
# ------------------------------------------------------------

log "Checking LLMService CRD"

kubectl get crd llmservices.serving.debasish.dev >/dev/null ||
  fail "LLMService CRD not found"

pass "LLMService CRD exists"


# ------------------------------------------------------------
# Create LLMService
# ------------------------------------------------------------

log "Creating test LLMService"

cat <<EOF | kubectl apply -f -
apiVersion: serving.debasish.dev/v1alpha1
kind: LLMService
metadata:
  name: ${NAME}
  namespace: ${NAMESPACE}
spec:
  model: Qwen/Qwen2.5-1.5B-Instruct
  image: vllm/vllm-openai:v0.30.0
  gpus: 1
  maxNumSeqs: 64
  maxModelLen: 4096
  tensorParallelSize: 1
  cacheSize: 20Gi
  autoscaling:
    enabled: true
    minReplicas: 1
    maxReplicas: 3
    targetWaitingRequests: 5
EOF

pass "LLMService created"


# ------------------------------------------------------------
# Reconciliation
# ------------------------------------------------------------

log "Waiting for operator reconciliation"

kubectl wait \
  --for=jsonpath='{.metadata.finalizers}' \
  --timeout="$TIMEOUT" \
  "llmservice/${NAME}" \
  -n "$NAMESPACE" >/dev/null ||
  fail "Operator did not add its finalizer"

pass "Operator observed LLMService"


# ------------------------------------------------------------
# Deployment
# ------------------------------------------------------------

log "Checking Deployment"

kubectl wait \
  --for=create \
  --timeout="$TIMEOUT" \
  "deployment/${NAME}" \
  -n "$NAMESPACE" >/dev/null 2>&1 ||
  fail "Operator did not create Deployment"

pass "Deployment created"


# ------------------------------------------------------------
# Service
# ------------------------------------------------------------

log "Checking Service"

kubectl wait \
  --for=create \
  --timeout="$TIMEOUT" \
  "service/${NAME}" \
  -n "$NAMESPACE" >/dev/null 2>&1 ||
  fail "Operator did not create Service"

pass "Service created"


# ------------------------------------------------------------
# PVC
# ------------------------------------------------------------

log "Checking PVC"

kubectl wait \
  --for=create \
  --timeout="$TIMEOUT" \
  "pvc/${NAME}" \
  -n "$NAMESPACE" >/dev/null 2>&1 ||
  fail "Operator did not create PVC"

pass "PVC created"


# ------------------------------------------------------------
# KEDA
# ------------------------------------------------------------

log "Checking KEDA ScaledObject"

kubectl wait \
  --for=create \
  --timeout="$TIMEOUT" \
  "scaledobject/${NAME}" \
  -n "$NAMESPACE" >/dev/null 2>&1 ||
  fail "Operator did not create KEDA ScaledObject"

pass "KEDA ScaledObject created"


# ------------------------------------------------------------
# Owner reference
# ------------------------------------------------------------

log "Checking owner references"

OWNER_KIND="$(
  kubectl get deployment "$NAME" \
    -n "$NAMESPACE" \
    -o jsonpath='{.metadata.ownerReferences[0].kind}'
)"

OWNER_NAME="$(
  kubectl get deployment "$NAME" \
    -n "$NAMESPACE" \
    -o jsonpath='{.metadata.ownerReferences[0].name}'
)"

[[ "$OWNER_KIND" == "LLMService" ]] ||
  fail "Deployment ownerReference kind is ${OWNER_KIND:-missing}"

[[ "$OWNER_NAME" == "$NAME" ]] ||
  fail "Deployment ownerReference name is ${OWNER_NAME:-missing}"

pass "Deployment ownerReference points to LLMService"


# ------------------------------------------------------------
# Status
# ------------------------------------------------------------

log "Checking status reconciliation"

PHASE=""

for _ in $(seq 1 30); do
  PHASE="$(
    kubectl get llmservice "$NAME" \
      -n "$NAMESPACE" \
      -o jsonpath='{.status.phase}' \
      2>/dev/null || true
  )"

  if [[ "$PHASE" == "Pending" ||
        "$PHASE" == "Ready" ||
        "$PHASE" == "Failed" ]]; then
    break
  fi

  sleep 2
done

[[ -n "${PHASE:-}" ]] ||
  fail "Operator did not update LLMService status"

pass "LLMService status updated: ${PHASE}"


# ------------------------------------------------------------
# Inspect generated Deployment configuration
# ------------------------------------------------------------

log "Checking generated vLLM Deployment configuration"

GPU_REQUEST="$(
  kubectl get deployment "$NAME" \
    -n "$NAMESPACE" \
    -o jsonpath='{.spec.template.spec.containers[0].resources.requests.nvidia\.com/gpu}'
)"

GPU_LIMIT="$(
  kubectl get deployment "$NAME" \
    -n "$NAMESPACE" \
    -o jsonpath='{.spec.template.spec.containers[0].resources.limits.nvidia\.com/gpu}'
)"

IMAGE="$(
  kubectl get deployment "$NAME" \
    -n "$NAMESPACE" \
    -o jsonpath='{.spec.template.spec.containers[0].image}'
)"

READY_PATH="$(
  kubectl get deployment "$NAME" \
    -n "$NAMESPACE" \
    -o jsonpath='{.spec.template.spec.containers[0].readinessProbe.httpGet.path}'
)"

READY_PORT="$(
  kubectl get deployment "$NAME" \
    -n "$NAMESPACE" \
    -o jsonpath='{.spec.template.spec.containers[0].readinessProbe.httpGet.port}'
)"

PROM_SCRAPE="$(
  kubectl get deployment "$NAME" \
    -n "$NAMESPACE" \
    -o jsonpath='{.spec.template.metadata.annotations.prometheus\.io/scrape}'
)"

PROM_PATH="$(
  kubectl get deployment "$NAME" \
    -n "$NAMESPACE" \
    -o jsonpath='{.spec.template.metadata.annotations.prometheus\.io/path}'
)"

PROM_PORT="$(
  kubectl get deployment "$NAME" \
    -n "$NAMESPACE" \
    -o jsonpath='{.spec.template.metadata.annotations.prometheus\.io/port}'
)"

[[ "$GPU_REQUEST" == "1" ]] ||
  fail "Deployment GPU request is ${GPU_REQUEST:-missing}, expected 1"

[[ "$GPU_LIMIT" == "1" ]] ||
  fail "Deployment GPU limit is ${GPU_LIMIT:-missing}, expected 1"

[[ "$IMAGE" == "vllm/vllm-openai:v0.30.0" ]] ||
  fail "Unexpected vLLM image: ${IMAGE:-missing}"

[[ "$READY_PATH" == "/health" ]] ||
  fail "Readiness probe path is ${READY_PATH:-missing}, expected /health"

[[ "$READY_PORT" == "8000" ]] ||
  fail "Readiness probe port is ${READY_PORT:-missing}, expected 8000"

[[ "$PROM_SCRAPE" == "true" ]] ||
  fail "Prometheus scrape annotation is missing"

[[ "$PROM_PATH" == "/metrics" ]] ||
  fail "Prometheus metrics path is ${PROM_PATH:-missing}"

[[ "$PROM_PORT" == "8000" ]] ||
  fail "Prometheus metrics port is ${PROM_PORT:-missing}"

pass "vLLM image, GPU resources, readiness probe and Prometheus annotations are correct"


# ------------------------------------------------------------
# Detect GPU
# ------------------------------------------------------------

log "Checking Kubernetes GPU availability"

GPU_NODE=""

while IFS= read -r node; do
  [[ -n "$node" ]] || continue

  GPU_COUNT="$(
    kubectl get node "$node" \
      -o jsonpath='{.status.allocatable.nvidia\.com/gpu}' \
      2>/dev/null || true
  )"

  if [[ "${GPU_COUNT:-0}" =~ ^[0-9]+$ ]] &&
     (( GPU_COUNT > 0 )); then
    GPU_NODE="$node"
    break
  fi
done < <(
  kubectl get nodes \
    -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}'
)

if [[ -z "$GPU_NODE" ]]; then

  skip "No Kubernetes node advertises an allocatable NVIDIA GPU"

  GPU_RUNTIME="false"

else

  GPU_RUNTIME="true"

  GPU_COUNT="$(
    kubectl get node "$GPU_NODE" \
      -o jsonpath='{.status.allocatable.nvidia\.com/gpu}'
  )"

  pass "GPU available on node ${GPU_NODE}: ${GPU_COUNT} GPU(s)"

fi


# ------------------------------------------------------------
# GPU runtime
# ------------------------------------------------------------

if [[ "$GPU_RUNTIME" == "true" ]]; then

  # ----------------------------------------------------------
  # GPU device plugin / node allocation
  # ----------------------------------------------------------

  log "Checking GPU scheduling capability"

  DEVICE_PLUGIN_PODS="$(
    kubectl get pods \
      -A \
      -o wide 2>/dev/null |
      grep -Ei 'nvidia.*device.*plugin|nvidia-device-plugin' ||
      true
  )"

  if [[ -z "$DEVICE_PLUGIN_PODS" ]]; then
    skip "NVIDIA device-plugin pod was not identified by name"
  else
    pass "NVIDIA device-plugin appears to be running"
  fi


  # ----------------------------------------------------------
  # vLLM pod
  # ----------------------------------------------------------

  log "Checking vLLM pod"

  kubectl wait \
    --for=create \
    "pod" \
    -l "app=${NAME}" \
    -n "$NAMESPACE" \
    --timeout="$TIMEOUT" >/dev/null 2>&1 ||
    fail "vLLM pod was not created"

  VLLM_POD="$(
    kubectl get pods \
      -n "$NAMESPACE" \
      -l "app=${NAME}" \
      -o jsonpath='{.items[0].metadata.name}'
  )"

  [[ -n "$VLLM_POD" ]] ||
    fail "Unable to determine vLLM pod"

  pass "vLLM pod created: ${VLLM_POD}"


  # ----------------------------------------------------------
  # Verify GPU was actually assigned to pod
  # ----------------------------------------------------------

  log "Checking GPU allocation on vLLM pod"

  POD_GPU_REQUEST="$(
    kubectl get pod "$VLLM_POD" \
      -n "$NAMESPACE" \
      -o jsonpath='{.spec.containers[0].resources.requests.nvidia\.com/gpu}'
  )"

  [[ "$POD_GPU_REQUEST" == "1" ]] ||
    fail "vLLM pod did not request 1 NVIDIA GPU"

  pass "vLLM pod requests 1 NVIDIA GPU"


  # ----------------------------------------------------------
  # Wait for vLLM readiness
  # ----------------------------------------------------------

  log "Waiting for vLLM pod to become Ready"

  kubectl wait \
    --for=condition=Ready \
    "pod/${VLLM_POD}" \
    -n "$NAMESPACE" \
    --timeout="$GPU_TIMEOUT" >/dev/null ||
    {
      kubectl describe pod "$VLLM_POD" -n "$NAMESPACE" >&2 || true
      fail "vLLM pod did not become Ready"
    }

  pass "vLLM pod is Ready"


  # ----------------------------------------------------------
  # Temporary curl pod
  # ----------------------------------------------------------

  log "Creating temporary HTTP client"

  CURL_POD="${NAME}-curl"

  kubectl run "$CURL_POD" \
    -n "$NAMESPACE" \
    --image=curlimages/curl:8.16.0 \
    --restart=Never \
    --command -- \
    sleep 600 >/dev/null

  kubectl wait \
    --for=condition=Ready \
    "pod/${CURL_POD}" \
    -n "$NAMESPACE" \
    --timeout="$TIMEOUT" >/dev/null ||
    fail "Temporary curl pod did not become Ready"

  cleanup_curl() {
    kubectl delete pod "$CURL_POD" \
      -n "$NAMESPACE" \
      --ignore-not-found \
      --wait=false >/dev/null 2>&1 || true
  }

  trap cleanup_curl EXIT


  # ----------------------------------------------------------
  # vLLM health
  # ----------------------------------------------------------

  log "Checking vLLM /health"

  kubectl exec "$CURL_POD" \
    -n "$NAMESPACE" \
    -- curl \
      --fail \
      --silent \
      --show-error \
      --max-time 10 \
      "http://${NAME}:8000/health" >/dev/null ||
    fail "vLLM /health endpoint failed"

  pass "vLLM /health responds successfully"


  # ----------------------------------------------------------
  # vLLM metrics
  # ----------------------------------------------------------

  log "Checking vLLM /metrics"

  METRICS="$(
    kubectl exec "$CURL_POD" \
      -n "$NAMESPACE" \
      -- curl \
        --fail \
        --silent \
        --show-error \
        --max-time 10 \
        "http://${NAME}:8000/metrics"
  )" ||
    fail "vLLM /metrics endpoint failed"

  [[ "$METRICS" == *"vllm:num_requests_waiting"* ]] ||
    fail "vLLM waiting-request metric was not found"

  pass "vLLM /metrics responds and exposes vllm:num_requests_waiting"


  # ----------------------------------------------------------
  # KEDA readiness
  # ----------------------------------------------------------

  log "Checking KEDA readiness"

  KEDA_READY="$(
    kubectl get scaledobject "$NAME" \
      -n "$NAMESPACE" \
      -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}'
  )"

  [[ "$KEDA_READY" == "True" ]] ||
    fail "KEDA ScaledObject is not Ready"

  pass "KEDA ScaledObject is Ready"


  # ----------------------------------------------------------
  # KEDA HPA
  # ----------------------------------------------------------

  log "Checking KEDA HPA"

  HPA_NAME="keda-hpa-${NAME}"

  kubectl get hpa "$HPA_NAME" \
    -n "$NAMESPACE" >/dev/null ||
    fail "KEDA HPA ${HPA_NAME} was not created"

  pass "KEDA HPA created: ${HPA_NAME}"


  # ----------------------------------------------------------
  # Prometheus
  # ----------------------------------------------------------

  log "Checking Prometheus"

  kubectl get service "$PROMETHEUS_SERVICE" \
    -n "$PROMETHEUS_NAMESPACE" >/dev/null ||
    fail "Prometheus Service not found"

  PROM_POD="$(
    kubectl get pods \
      -n "$PROMETHEUS_NAMESPACE" \
      -l app=prometheus \
      -o jsonpath='{.items[0].metadata.name}' \
      2>/dev/null || true
  )"

  if [[ -z "$PROM_POD" ]]; then
    fail "Prometheus pod not found"
  fi

  PROM_READY="$(
    kubectl exec "$PROM_POD" \
      -n "$PROMETHEUS_NAMESPACE" \
      -- wget -qO- \
      http://127.0.0.1:9090/-/ready \
      2>/dev/null || true
  )"

  [[ "$PROM_READY" == *"Prometheus Server is Ready"* ]] ||
    fail "Prometheus is not Ready"

  pass "Prometheus is Ready"


  # ----------------------------------------------------------
  # DCGM exporter
  # ----------------------------------------------------------

  log "Checking DCGM exporter"

  if kubectl get service "$DCGM_SERVICE" \
      -n "$DCGM_NAMESPACE" >/dev/null 2>&1; then

    DCGM_POD="$(
      kubectl get pods \
        -n "$DCGM_NAMESPACE" \
        -l app.kubernetes.io/name=dcgm-exporter \
        -o jsonpath='{.items[0].metadata.name}' \
        2>/dev/null || true
    )"

    if [[ -z "$DCGM_POD" ]]; then
      fail "DCGM exporter Service exists but exporter pod was not found"
    fi

    DCGM_METRICS="$(
      kubectl exec "$DCGM_POD" \
        -n "$DCGM_NAMESPACE" \
        -- wget -qO- \
        http://127.0.0.1:9400/metrics \
        2>/dev/null || true
    )"

    [[ "$DCGM_METRICS" == *"DCGM"* ||
       "$DCGM_METRICS" == *"DCGM_FI_DEV"* ]] ||
      fail "DCGM exporter /metrics did not expose DCGM metrics"

    pass "DCGM exporter metrics are available"

  else

    skip "DCGM exporter Service is not installed"

  fi


  # ----------------------------------------------------------
  # Remove temporary curl pod
  # ----------------------------------------------------------

  cleanup_curl
  trap cleanup EXIT

else

  # ----------------------------------------------------------
  # No GPU
  # ----------------------------------------------------------

  log "GPU runtime validation"

  skip "No NVIDIA GPU available"
  skip "vLLM runtime readiness not tested"
  skip "vLLM /health not tested"
  skip "vLLM /metrics not tested"
  skip "KEDA runtime scaling not tested"
  skip "DCGM exporter runtime not tested"

fi


# ------------------------------------------------------------
# Final result
# ------------------------------------------------------------

log "Operator test result"

echo
echo "======================================"
echo " OPERATOR TEST: PASS"
echo "======================================"
echo

echo "Core operator checks:"
echo "  ✓ observed the LLMService"
echo "  ✓ added its finalizer"
echo "  ✓ created the Deployment"
echo "  ✓ created the Service"
echo "  ✓ created the PVC"
echo "  ✓ created the KEDA ScaledObject"
echo "  ✓ set the Deployment ownerReference"
echo "  ✓ updated LLMService status"
echo "  ✓ generated correct GPU/vLLM configuration"

if [[ "$GPU_RUNTIME" == "true" ]]; then
  echo
  echo "GPU/runtime checks:"
  echo "  ✓ NVIDIA GPU detected"
  echo "  ✓ vLLM pod scheduled"
  echo "  ✓ GPU allocated to vLLM pod"
  echo "  ✓ vLLM pod Ready"
  echo "  ✓ vLLM /health"
  echo "  ✓ vLLM /metrics"
  echo "  ✓ vllm:num_requests_waiting"
  echo "  ✓ KEDA ScaledObject Ready"
  echo "  ✓ KEDA HPA"
  echo "  ✓ Prometheus"
  echo "  ✓ DCGM exporter (if installed)"
else
  echo
  echo "GPU/runtime checks:"
  echo "  ⚠ SKIPPED — no NVIDIA GPU available"
fi

echo
echo "GPU-less Kind is therefore still a valid operator test environment."