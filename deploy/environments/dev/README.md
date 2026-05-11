# Development Environment Overlays

Kustomize overlays for deploying vLLM P/D (Prefill/Decode) inference locally.
This repo ships a single scenario — always P/D — so there is one scenario directory.
The atomic components it composes live in `deploy/components/`:

| Component | Description |
|-----------|-------------|
| `vllm-prefill/` | Prefill pod — labelled `app: ${PREFILL_POOL_NAME}` |
| `vllm-decode/` | Decode pod — labelled `app: ${DECODE_POOL_NAME}`, includes routing sidecar |
| `overlays/simulator/` | Adds `--mode=${VLLM_SIM_MODE}`, UDS tokenizer, KV cache and ZMQ args (ZMQ endpoint points at the decode EPP, `${EPP_NAME_D}`) |

These overlays are used by `scripts/kind-dev-env.sh` (via `kustomize build` + env var
substitution with `envsubst`).

## Scenario Directory

| Directory | Components | Description |
|-----------|-----------|-------------|
| `p-d/` | prefill + decode | Separate Prefill and Decode pods, each behind its own `InferencePool` and own EPP. Client hits `/prefill/...` or `/decode/...` on the Gateway NodePort |

Unlike `llm-d-inference-scheduler`, there are no `epd/`, `e-pd/`, or `e-p-d/`
directories — encode-disaggregation is out of scope here.

Data parallel (`VLLM_DATA_PARALLEL_SIZE`) is orthogonal and combines with P/D —
set it to `2`+ to enable multi-rank routing within both pools.

## Shared Infrastructure

| Directory | Description |
|-----------|-------------|
| `base-kind-istio/` | Istio control plane + inference gateway (two EPP Deployments, two InferencePools, two HTTPRoutes with `PathPrefix` matching `/prefill` and `/decode`, RBAC, Gateway). Applied separately by `kind-dev-env.sh` before the `p-d/` scenario overlay |

## Deploy

```bash
# Default: TinyLlama simulator on P/D
make env-dev-kind

# Different gateway host port (default 30080; baked in at cluster creation)
KIND_GATEWAY_HOST_PORT=31080 make env-dev-kind

# Data parallel
VLLM_DATA_PARALLEL_SIZE=2 make env-dev-kind

# Different model
MODEL_NAME=meta-llama/Llama-3.1-8B-Instruct make env-dev-kind
```

## Key Environment Variables

Variables substituted at deploy time via `envsubst`:

| Variable | Description | Default |
|----------|-------------|---------|
| `MODEL_NAME` | Model name passed to vLLM | `TinyLlama/TinyLlama-1.1B-Chat-v1.0` |
| `PREFILL_POOL_NAME` | Prefill InferencePool name + `app:` label on vllm-p pods | `${MODEL_NAME_SAFE}-prefill-pool` |
| `DECODE_POOL_NAME` | Decode InferencePool name + `app:` label on vllm-d pods | `${MODEL_NAME_SAFE}-decode-pool` |
| `DISAGG_P` | Deploy a separate Prefill pod; only `true` is supported | `true` |
| `EPP_NAME_P` | Prefill EPP Deployment/Service/SA name | `${MODEL_NAME_SAFE}-prefill-endpoint-picker` |
| `EPP_NAME_D` | Decode EPP Deployment/Service/SA name | `${MODEL_NAME_SAFE}-decode-endpoint-picker` |
| `EPP_IMAGE` | EPP image (the llm-d inference scheduler) | `ghcr.io/llm-d/llm-d-inference-scheduler:dev` |
| `VLLM_IMAGE` | vLLM container image (simulator or real) | `ghcr.io/llm-d/llm-d-inference-sim:v0.8.2` |
| `SIDECAR_IMAGE` | Routing sidecar image | `ghcr.io/llm-d/llm-d-routing-sidecar:dev` |
| `UDS_TOKENIZER_IMAGE` | UDS tokenizer sidecar image | `ghcr.io/llm-d/llm-d-uds-tokenizer:dev` |
| `VLLM_SIM_MODE` | Simulator response mode: `echo` or `random` | `echo` |
| `VLLM_REPLICA_COUNT_P` | Prefill deployment replicas | `1` |
| `VLLM_REPLICA_COUNT_D` | Decode deployment replicas | `1` |
| `VLLM_DATA_PARALLEL_SIZE` | Data parallel rank count per vLLM pod — applies to both prefill and decode | `1` |
| `CONNECTOR_TYPE` / `KV_CONNECTOR_TYPE` | KV connector for P/D (used by the routing-sidecar on decode) | `nixlv2` |
| `HF_TOKEN` | HuggingFace token for downloading real models (empty for simulator) | `` |
| `NAMESPACE` | Kubernetes namespace for all resources | `default` |
| `KIND_GATEWAY_HOST_PORT` | Host port mapped to the Gateway NodePort 30080 (baked into the cluster at creation — recreate to change) | `30080` |
| `VLLM_EXTRA_ARGS_P` | Extra flags appended to Prefill vLLM args (`--flag=value` format) | `` |
| `VLLM_EXTRA_ARGS_D` | Extra flags appended to Decode vLLM args (`--flag=value` format) | `` |

## EPP Configs

Unlike llm-d's single multi-profile config, each EPP uses its own single-profile config
from `deploy/config/`:

| Config | Loaded into ConfigMap | Consumed by |
|--------|----------------------|-------------|
| `sim-prefill-epp-config.yaml` | `epp-config-p` | Prefill EPP (`${EPP_NAME_P}` Deployment) |
| `sim-decode-epp-config.yaml` | `epp-config-d` | Decode EPP (`${EPP_NAME_D}` Deployment) |

Both use `single-profile-handler` with `prefill-filter` or `decode-filter` respectively.
Neither uses `prepareDataPlugins` / `disagg-profile-handler` — cross-pool scheduling
intelligence is out of scope for the EPP in this repo.
