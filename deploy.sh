#!/bin/bash
set -euo pipefail

log() { echo -e "\n\033[1;36m▶ $*\033[0m"; }

echo "=========================================================="
echo "   CLOUDNATIVE FEED — DEPLOYMENT"
echo "=========================================================="

CLUSTER_TYPE="unknown"
kubectl get nodes -o wide | grep -qi k3s && CLUSTER_TYPE=k3s
kubectl get nodes -o wide | grep -qi kind && CLUSTER_TYPE=kind
kubectl get nodes -o wide | grep -qi minikube && CLUSTER_TYPE=minikube
log "Cluster type: $CLUSTER_TYPE"

log "[1/7] Seeding 15 sample images..."
mkdir -p public-app/static/images
for i in $(seq 1 15); do
  curl -sfL "https://picsum.photos/600/600?random=$i" \
       -o "public-app/static/images/photo-$i.jpg" || true
done
echo "   images: $(ls public-app/static/images | wc -l)"

log "[2/7] Building Docker image..."
docker build -t public-app:latest public-app/

log "[3/7] Loading image into cluster runtime..."
case "$CLUSTER_TYPE" in
  k3s)
    docker save public-app:latest | sudo k3s ctr images import - ;;
  kind)
    kind load docker-image public-app:latest ;;
  minikube)
    minikube image load public-app:latest ;;
  *)
    echo "⚠️  Unknown — trying k3s import"
    docker save public-app:latest | sudo k3s ctr images import - || true ;;
esac

log "[4/7] Creating DB init ConfigMap..."
kubectl create configmap db-init-config \
  --from-file=init-db/01-init.sql \
  --dry-run=client -o yaml | kubectl apply -f -

log "[5/7] Applying manifests..."
kubectl apply -f k8s/01-db.yaml
kubectl rollout status deployment/postgres --timeout=180s

kubectl apply -f k8s/02-app.yaml
kubectl apply -f k8s/03-hpa.yaml
kubectl apply -f k8s/04-monitoring.yaml
kubectl rollout status deployment/public-app --timeout=180s

log "[6/7] Installing metrics-server (for HPA)..."
kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml
kubectl -n kube-system patch deployment metrics-server --type=json \
  -p='[{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--kubelet-insecure-tls"}]' || true

log "[7/7] Verifying..."
sleep 5
kubectl get svc public-app-service grafana-service prometheus-service
kubectl get hpa public-app-hpa

echo ""
echo "=========================================================="
echo "      ✅ DEPLOYMENT COMPLETE — USE TRAFFIC PORTS"
echo "=========================================================="
echo "  Feed (App)     →  Port 30080"
echo "  Grafana        →  Port 30300  (admin/admin)"
echo "  Prometheus     →  Port 30900"
echo "=========================================================="