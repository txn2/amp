![amp logo](amp.png)

# Kubernetes Admission Mutation Proxy (amp)

`amp` is a Kubernetes [Dynamic Admission Control](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/) mutating webhook **proxy** for Pods.

`amp` receives Kubernetes Admission Review requests for Pod creation events from any Namespace labeled `amp.txn2.com/enabled=true` and forwards the [Pod definition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.18/#pod-v1-core) as a JSON POST to a custom HTTP endpoint defined as the value of the Namespace annotation `amp.txn2.com/ep`.  The custom HTTP endpoint receives a Pod definition for evaluation and returns an array of [JSONPatch](http://jsonpatch.com/) operations to `amp` (see [example](https://github.com/txn2/amp-wh-example/blob/c58b545f9739b95a110ff22eac1ec6c47a4943a4/amp_wh_example.go#L113)).

![amp flow depiction](amp-flow.png)




## Example Implementation

Refer to the example implementation at [txn2/amp-wh-example](https://github.com/txn2/amp-wh-example).

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

Replace the `caBundle` key in `./k8s/80-webhook.yml` with the value returned from the previous command and apply the following webhook configuration:

```shell script
kubectl apply -f ./k8s/80-webhook.yml
```

## Development

### Release
```bash
goreleaser --skip-publish --rm-dist --skip-validate
```

```bash
GITHUB_TOKEN=$GITHUB_TOKEN goreleaser --rm-dist
```
