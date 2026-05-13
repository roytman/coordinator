# Kind Development

Documentation for running the coordinator's P/D dev environment locally on a Kind cluster.

## Table of Contents

- [Overview](#overview)
- [Requirements](#requirements)
- [Kind Development Environment](#kind-development-environment)
  - [Accessing the Gateway](#accessing-the-gateway)
  - [Testing the Prefill and Decode Paths](#testing-the-prefill-and-decode-paths)
  - [Cleanup](#cleanup)
- [Image Tags and Overrides](#image-tags-and-overrides)

## Overview

_TBD_

## Requirements

- [Make](https://www.gnu.org/software/make/) `v4`+
- [Docker](https://www.docker.com/) (or [Podman](https://podman.io/))
- [Kubernetes in Docker (KIND)](https://github.com/kubernetes-sigs/kind)
- [Kubectl](https://kubectl.docs.kubernetes.io/installation/kubectl/) `v1.25`+

## Kind Development Environment

```bash
make env-dev-kind
```

Deploys a local inference stack to the `llm-d-coordinator-dev` Kind cluster. This script bootstraps all prerequisites (Gateway API, GIE, Istio CRDs, and the Istio control plane) in the default namespace.

The disaggregation topology is selected via `DISAGG_TOPOLOGY` (default: `pd`):

| `DISAGG_TOPOLOGY` | Pools deployed | Default model |
|---|---|---|
| `pd` (default) | prefill, decode | `TinyLlama/TinyLlama-1.1B-Chat-v1.0` |
| `epd` | encode, prefill, decode | `Qwen/Qwen3-VL-2B-Instruct` |

**P/D (default)** — two distinct inference pipelines:

- `/prefill/...` → prefill InferencePool → EPP-P → vllm-p pods
- `/decode/...`  → decode InferencePool  → EPP-D → vllm-d pods

**E/P/D** — three distinct inference pipelines:

```bash
DISAGG_TOPOLOGY=epd make env-dev-kind
```

- `/encode/...`  → encode InferencePool  → EPP-E → vllm-e pods
- `/prefill/...` → prefill InferencePool → EPP-P → vllm-p pods
- `/decode/...`  → decode InferencePool  → EPP-D → vllm-d pods

> [!NOTE]
> Pre-pull external images to avoid slow downloads during deploy:
> ```
> docker pull ghcr.io/llm-d/llm-d-inference-sim:v0.8.2
> docker pull ghcr.io/llm-d/llm-d-inference-scheduler:dev
> docker pull ghcr.io/llm-d/llm-d-routing-sidecar:v0.8.0
> docker pull ghcr.io/llm-d/llm-d-uds-tokenizer:v0.8.0
> ```

### Accessing the Gateway

The Gateway is exposed on your development machine at `http://localhost:30080` via a
NodePort baked into the Kind cluster at creation time. Override the host port with:

```bash
KIND_GATEWAY_HOST_PORT=31080 make env-dev-kind
```

> [!NOTE]
> `KIND_GATEWAY_HOST_PORT` is baked in at cluster creation. To change it after the
> cluster exists, run `make clean-env-dev-kind` first, then recreate.

Alternatively, port-forward the Istio gateway service:

```bash
kubectl --context kind-llm-d-coordinator-dev \
  port-forward service/inference-gateway-istio 8080:80
```

Then requests go to `http://localhost:8080`.

### Testing the Prefill and Decode Paths (P/D topology)

The Gateway routes by path prefix — each path hits its own pool:

```bash
# Test prefill path
curl -s http://localhost:30080/prefill/v1/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"TinyLlama/TinyLlama-1.1B-Chat-v1.0","prompt":"hi","max_tokens":5}' | jq

# Test decode path
curl -s http://localhost:30080/decode/v1/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"TinyLlama/TinyLlama-1.1B-Chat-v1.0","prompt":"hi","max_tokens":5}' | jq
```

The URLRewrite filter strips the path prefix before forwarding to the vLLM pods,
so each pool sees the standard `/v1/completions` path.

Inspect cluster state:

```bash
kubectl get inferencepools
kubectl get pods -l llm-d.ai/component=prefill
kubectl get pods -l llm-d.ai/component=decode
kubectl logs -l app=$(echo $MODEL_NAME_SAFE)-prefill-endpoint-picker -c epp
kubectl logs -l app=$(echo $MODEL_NAME_SAFE)-decode-endpoint-picker  -c epp
```

### Testing the Encode, Prefill and Decode Paths (E/P/D topology)

```bash
# Test encode path
curl -s http://localhost:30080/encode/v1/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"Qwen/Qwen3-VL-2B-Instruct","prompt":"hi","max_tokens":5}' | jq

# Test prefill path
curl -s http://localhost:30080/prefill/v1/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"Qwen/Qwen3-VL-2B-Instruct","prompt":"hi","max_tokens":5}' | jq

# Test decode path
curl -s http://localhost:30080/decode/v1/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"Qwen/Qwen3-VL-2B-Instruct","prompt":"hi","max_tokens":5}' | jq
```

Inspect cluster state:

```bash
kubectl get inferencepools
kubectl get pods -l llm-d.ai/component=encode
kubectl get pods -l llm-d.ai/component=prefill
kubectl get pods -l llm-d.ai/component=decode
kubectl logs -l app=$(echo $MODEL_NAME_SAFE)-encode-endpoint-picker  -c epp
kubectl logs -l app=$(echo $MODEL_NAME_SAFE)-prefill-endpoint-picker -c epp
kubectl logs -l app=$(echo $MODEL_NAME_SAFE)-decode-endpoint-picker  -c epp
```

### Cleanup

```bash
make clean-env-dev-kind
```

Deletes the Kind cluster entirely.

## Image Tags and Overrides

Image tags and registry live in [versions.mk](versions.mk), which the Makefile includes
and exports to `scripts/kind-dev-env.sh`:

| Tag variable | Full image variable | Image | Default tag |
|---|---|---|---|
| `VLLM_SIMULATOR_TAG` | `VLLM_IMAGE` | `llm-d-inference-sim` | `v0.8.2` |
| `EPP_TAG` | `EPP_IMAGE` | `llm-d-inference-scheduler` | `dev` |
| `SIDECAR_TAG` | `SIDECAR_IMAGE` | `llm-d-routing-sidecar` | `v0.8.0` |
| `UDS_TOKENIZER_TAG` | `UDS_TOKENIZER_IMAGE` | `llm-d-uds-tokenizer` | `v0.8.0` |
| `IMAGE_REGISTRY` | — | — | `ghcr.io/llm-d` |

Override any tag via the command line — Make's `?=` assignments yield to existing env
or command-line values:

```bash
EPP_TAG=foo make env-dev-kind
# or permanently by editing versions.mk
```
