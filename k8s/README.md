# AMP System

Create the `amp-system` Kubernetes namespace.
```shell
kubectl apply -f ./00-namespace.yml
```

Create the `amp-system` ServiceAccount, ClusterRole and ClusterRoleBinding
```shell
kubectl apply -f ./01-rbac.yml
```

Create the `amp-system` Service
```shell
kubectl apply -f ./10-service.yml
```

If using cert-manager (recommended), see ./000-cert-manager/README.md

Create `server` certificate for AMP
```shell
kubectl apply -f ./21-certificate-webhook-server.yml
```

Create `client` certificate for MutatingWebhookConfiguration
```shell
kubectl apply -f ./22-certificate-webhook-client.yml
```

Create AMP deployment:
```shell
kubectl apply -f 80-webhook.yml
```

Create MutatingWebhookConfiguration:

```shell
kubectl apply -f 80-webhook.yml
```