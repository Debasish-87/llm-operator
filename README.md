# llm-operator

A Kubernetes operator that deploys and manages [vLLM](https://github.com/vllm-project/vllm) inference servers through a single `LLMService` custom resource.

## Features

- **One CR, full stack** – creates the Deployment, Service, and model-cache PVC for you
- **GPU-aware** – requests `nvidia.com/gpu`, tolerates GPU taints, sets up `/dev/shm` for tensor parallelism
- **Autoscaling** – optional [KEDA](https://keda.sh) `ScaledObject` driven by vLLM's waiting-request metric
- **Private models** – injects `HF_TOKEN` from a Kubernetes Secret
- **Observability** – Prometheus scrape annotations, plus Prometheus / Grafana / DCGM manifests in `deploy/monitoring`
- **Status reporting** – `phase`, `readyReplicas`, `endpoint`, and `Ready` condition

## Quick Start

**Requirements:** Go 1.25+, Docker, kubectl, a Kubernetes cluster (GPU nodes needed to actually run vLLM).

```sh
# Local Kind cluster + KEDA + monitoring + operator, in one command
./setup-llm-operator.sh

# Or manually, against your current cluster
make install                                   # install the CRD
make deploy IMG=<registry>/llm-operator:tag    # deploy the operator
kubectl apply -f examples/serving_v1alpha1_llmservice.yaml
```

Check the result:

```sh
kubectl get llmservices
kubectl get deploy,svc,pvc
```

> On a cluster without GPUs the vLLM pod stays `Pending`. This is expected; the operator itself still works.

## Example

```yaml
apiVersion: serving.debasish.dev/v1alpha1
kind: LLMService
metadata:
  name: qwen-demo
spec:
  model: Qwen/Qwen2.5-1.5B-Instruct
  gpus: 1
  cacheSize: 20Gi
  hfTokenSecretRef:          # optional
    name: hf-token
    key: token
  autoscaling:               # optional, requires KEDA
    enabled: true
    minReplicas: 1
    maxReplicas: 3
    targetWaitingRequests: 5
```

Once ready, the OpenAI-compatible API is available at `status.endpoint`
(`http://<name>.<namespace>.svc.cluster.local:8000`).

## Spec Reference

| Field | Default | Description |
|---|---|---|
| `model` | – (required) | Hugging Face model ID |
| `image` | `vllm/vllm-openai:v0.30.0` | vLLM container image |
| `gpus` | `1` | GPUs per pod |
| `maxNumSeqs` | `64` | vLLM `--max-num-seqs` |
| `maxModelLen` | `4096` | vLLM `--max-model-len` |
| `tensorParallelSize` | `1` | vLLM `--tensor-parallel-size` |
| `cacheSize` | `20Gi` | Model cache PVC size |
| `hfTokenSecretRef` | – | Secret name/key (key defaults to `token`) |
| `autoscaling.*` | disabled | `enabled`, `minReplicas`, `maxReplicas`, `targetWaitingRequests` |

## Development

```sh
make test         # unit + envtest suite
make lint         # golangci-lint
make run          # run the controller locally against your kubeconfig
make manifests generate   # regenerate CRDs / deepcopy after editing API types
./test-operator.sh        # end-to-end validation (GPU checks skipped when no GPU)
```

## Project Layout

```
api/v1alpha1/          LLMService API types
internal/controller/   Reconciler and resource builders (deployment, service, pvc, scaledobject, status)
config/                CRD, RBAC, manager, and sample manifests (kustomize)
deploy/monitoring/     Prometheus, Grafana, DCGM exporter
examples/              Ready-to-apply LLMService examples
```

## Uninstall

```sh
kubectl delete llmservices --all
make undeploy
make uninstall
```

## License

Apache License 2.0. See the header in each source file.
