#!/bin/bash

set -e

ENVIRONMENT=${1:-staging}

echo "🚀 Deploying to $ENVIRONMENT..."

# Build and tag images
echo "🐳 Building Docker images..."
make docker-build

# Push images
echo "📤 Pushing images to registry..."
make docker-push

# Deploy based on environment
if [ "$ENVIRONMENT" = "staging" ]; then
    echo "📦 Deploying to staging cluster..."
    kubectl apply -k infrastructure/kubernetes/overlays/staging/
elif [ "$ENVIRONMENT" = "production" ]; then
    echo "📦 Deploying to production cluster..."
    kubectl apply -k infrastructure/kubernetes/overlays/production/
else
    echo "❌ Unknown environment: $ENVIRONMENT"
    exit 1
fi

echo "✅ Deployment complete!"
echo "🔍 Check deployment status:"
echo "  kubectl get pods -n blockchain-monitor"
