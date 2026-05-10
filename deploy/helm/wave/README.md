# wave Helm chart

Minimal Helm chart for deploying wave to Kubernetes.

## Quick start

```sh
helm install my-svc deploy/helm/wave \
  --set image.tag=v1.0.1 \
  --set-file serverYaml=./server.yaml
```

## What you get

- **Deployment** with 2 replicas (HPA optional)
- **Service** on ClusterIP:8080
- **ConfigMap** for `server.yaml` (or point at an existing one with `existingConfigMap`)
- **PodDisruptionBudget** at `minAvailable: 1` for safe rolling updates
- **Liveness/readiness probes** wired to wave's `/healthz` and `/readyz`
- **Distroless-friendly security context** (non-root, read-only root FS, drop ALL caps)
- **Optional Ingress** behind any IngressClass
- **Optional ServiceMonitor** for kube-prometheus-stack scraping `/metrics`

## Customizing config

Inline:
```sh
helm install my-svc deploy/helm/wave \
  --set-file serverYaml=./my-server.yaml
```

Or manage the ConfigMap externally:
```sh
kubectl create configmap wave-config --from-file=server.yaml=./server.yaml
helm install my-svc deploy/helm/wave --set existingConfigMap=wave-config
```

## Secrets

Never inline secrets in `values.yaml`. Use `env[*].valueFrom.secretKeyRef`:

```yaml
env:
  - name: OIDC_CLIENT_ID
    valueFrom: { secretKeyRef: { name: wave-secrets, key: oidc_client_id } }
  - name: STRIPE_WEBHOOK_SECRET
    valueFrom: { secretKeyRef: { name: wave-secrets, key: stripe_webhook } }
```

Then in `server.yaml` use `${ENV:OIDC_CLIENT_ID}` (handled by infra/secrets).
