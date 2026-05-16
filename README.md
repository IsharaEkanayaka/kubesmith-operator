# Operator

A minimal Kubernetes operator scaffold for the Application abstraction.

## What this includes
- Go module with controller-runtime
- Application API type (platform.kubesmith.io/v1alpha1)
- Controller skeleton
- Basic Dockerfile and Makefile

## Next steps
- Implement Argo CD Application reconciliation
- Add ServiceMonitor reconciliation
- Add RBAC and namespace authorization logic
