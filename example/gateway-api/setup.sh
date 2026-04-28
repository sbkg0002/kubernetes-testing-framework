#!/usr/bin/env bash
# Sets up the prerequisites for the gateway-api example on a kind cluster.
# Run once after `make cluster-up` (or `kind create cluster`), before `ktf run`.
#
# Usage: bash example/gateway-api/setup.sh

set -euo pipefail

GATEWAY_API_VERSION="v1.2.1"

echo "==> Installing Gateway API CRDs (${GATEWAY_API_VERSION})"
kubectl apply -f "https://github.com/kubernetes-sigs/gateway-api/releases/download/${GATEWAY_API_VERSION}/standard-install.yaml"

echo "==> Waiting for CRDs to be established"
kubectl wait --for=condition=Established \
  crd/gateways.gateway.networking.k8s.io \
  crd/gatewayclasses.gateway.networking.k8s.io \
  crd/httproutes.gateway.networking.k8s.io \
  --timeout=60s

echo "==> Starting cloud-provider-kind (provides GatewayClass + LoadBalancer IPs)"
if docker ps --filter name=cloud-provider-kind --format '{{.Names}}' | grep -q cloud-provider-kind; then
  echo "    cloud-provider-kind already running, skipping"
else
  VERSION=$(basename "$(curl -Ls -o /dev/null -w '%{url_effective}' \
    https://github.com/kubernetes-sigs/cloud-provider-kind/releases/latest)")
  docker run -d --name cloud-provider-kind --rm --network host \
    -v /var/run/docker.sock:/var/run/docker.sock \
    "registry.k8s.io/cloud-provider-kind/cloud-controller-manager:${VERSION}"
fi

echo "==> Waiting for GatewayClass 'cloud-provider-kind' to be accepted"
for i in $(seq 1 24); do
  status=$(kubectl get gatewayclass cloud-provider-kind \
    -o jsonpath='{.status.conditions[?(@.type=="Accepted")].status}' 2>/dev/null || true)
  if [ "$status" = "True" ]; then
    echo "    GatewayClass accepted"
    break
  fi
  echo "    attempt ${i}/24, waiting 5s..."
  sleep 5
done

echo ""
echo "Prerequisites ready. Run the suite with:"
echo "  bin/ktf run --config example/gateway-api.ktf.yaml"
