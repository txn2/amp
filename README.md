# Kubernetes Admission Mutation Proxy (amp)

`amp` is a Kubernetes [Dynamic Admission Control](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/) mutating webhook **proxy** for Pods.


## Install

```shell script
git clone git@github.com:txn2/amp.git
cd amp

# create amp-system namespace
kubectl apply -f ./k8s/00-namespace.yml
```

Create Certificate as Kubernets Secret in the new `amp-system` Namespace:

```shell script
curl https://raw.githubusercontent.com/morvencao/kube-mutating-webhook-tutorial/master/deployment/webhook-create-signed-cert.sh -o cert-gen.sh

chmod 775 cert-gen.sh

./cert-gen.sh --service amp --namespace amp-system --secret amp-cert
```

Create RBAC access controls, a Service and `amp` Deployment:
```shell script
# setup rbac for apm
kubectl apply -f ./k8s/01-rbac.yml

# create the amp service used by the webhook configuration
kubectl apply -f ./k8s/10-service.yml

# create the amp deployment
kubectl apply -f ./k8s/30-deployment.yml
```

Create a `caBundle` required for the `./k8s/80-webhook.yml` configuration:
```shell script
kubectl config view --raw --minify --flatten -o jsonpath='{.clusters[].cluster.certificate-authority-data}'
```

Replace the `caBundle` key in `./k8s/80-webhook.yml` with the value return from the previous command and apply the webhook configuration:

```shell script
kubectl apply -f ./k8s/80-webhook.yml
```

## Example

Refer to the example implementation at [txn2/amp-wh-example](https://github.com/txn2/amp-wh-example).

## Development

### Release
```bash
goreleaser --skip-publish --rm-dist --skip-validate
```

```bash
GITHUB_TOKEN=$GITHUB_TOKEN goreleaser --rm-dist
```
