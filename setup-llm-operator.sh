#!/usr/bin/env bash
set -Eeuo pipefail

# One-command bootstrap for the llm-operator project.
# Intended for a local Kind development cluster and later reusable for GPU validation.

CLUSTER_NAME="${KIND_CLUSTER:-llm-operator}"
IMAGE="${IMG:-example.com/llm-operator:v0.0.1}"
KEDA_VERSION="${KEDA_VERSION:-2.18.2}"

log() { printf '\n==> %s\n' "$*"; }
fail() { echo "ERROR: $*" >&2; exit 1; }

command -v kubectl >/dev/null || fail "kubectl is required"
command -v kind >/dev/null || fail "kind is required"
command -v make >/dev/null || fail "make is required"
command -v docker >/dev/null || fail "docker is required"

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT_DIR"

log "Checking Docker"
docker info >/dev/null 2>&1 || fail "Docker daemon is not running"

log "Creating/reusing Kind cluster: ${CLUSTER_NAME}"
if ! kind get clusters | grep -qx "$CLUSTER_NAME"; then
  kind create cluster --name "$CLUSTER_NAME"
fi
kubectl config use-context "kind-${CLUSTER_NAME}" >/dev/null
kubectl cluster-info >/dev/null

log "Generating manifests"
make manifests generate

log "Installing LLMService CRD"
make install

log "Installing KEDA ${KEDA_VERSION}"
kubectl apply --server-side -f "https://github.com/kedacore/keda/releases/download/v${KEDA_VERSION}/keda-${KEDA_VERSION}.yaml"
kubectl rollout status deployment/keda-operator -n keda --timeout=180s
kubectl rollout status deployment/keda-metrics-apiserver -n keda --timeout=180s
kubectl rollout status deployment/keda-admission -n keda --timeout=180s

log "Installing Prometheus"
kubectl apply -k deploy/monitoring/prometheus
kubectl rollout status deployment/prometheus -n monitoring --timeout=180s

log "Installing Grafana"
kubectl apply -k deploy/monitoring/grafana
kubectl rollout status deployment/grafana -n monitoring --timeout=180s

log "Building operator image: ${IMAGE}"
make docker-build IMG="${IMAGE}"

log "Loading operator image into Kind"
kind load docker-image "${IMAGE}" --name "${CLUSTER_NAME}"

log "Deploying operator"
make deploy IMG="${IMAGE}"
kubectl rollout status deployment/llm-operator-controller-manager -n llm-operator-system --timeout=180s

log "Creating sample LLMService"
kubectl apply -f config/samples/serving_v1alpha1_llmservice.yaml

log "Waiting for operator reconciliation"
kubectl wait --for=jsonpath='{.metadata.generation}'=1 llmservice/llmservice-sample --timeout=60s >/dev/null 2>&1 || true

log "Current project state"
echo
kubectl get llmservices
kubectl get deployment,svc,pvc
kubectl get scaledobjects 2>/dev/null || true
kubectl get pods -A

echo
cat <<STATE
Setup complete.

Cluster:        ${CLUSTER_NAME}
Operator image: ${IMAGE}
KEDA:           ${KEDA_VERSION}

If this machine has no NVIDIA GPU, the vLLM pod is expected to remain Pending because
LLMService requests nvidia.com/gpu. That is not an operator setup failure.

Useful checks:
  kubectl get llmservice llmservice-sample -o yaml
  kubectl get pods
  kubectl describe pod -l app=llmservice-sample
  kubectl get scaledobject llmservice-sample -o yaml
  kubectl get pods -n monitoring
STATE
