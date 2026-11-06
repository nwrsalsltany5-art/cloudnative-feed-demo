# CloudNative Feed — Kubernetes Demo

Mobile-first social feed demo on Kubernetes with HPA autoscaling and Prometheus/Grafana observability.

## Stack
- Backend: Go 1.22 + PostgreSQL
- Frontend: HTML/CSS/JS (single-page)
- Orchestration: Kubernetes (k3s)
- Autoscaling: HPA v2 (2 → 10 pods at 50% CPU)
- Observability: Prometheus + Grafana
- Load testing: k6

## Quick Start

```bash
chmod +x deploy.sh
./deploy.sh
```

## Access (Traffic Ports)
| Service | Port |
|---|---|
| Feed (App) | 30080 |
| Grafana | 30300 (admin/admin) |
| Prometheus | 30900 |

## Autoscale test
```bash
kubectl apply -f k8s/05-k6-load.yaml
kubectl get hpa public-app-hpa -w
```

## Cleanup
```bash
kubectl delete -f k8s/ --ignore-not-found
```