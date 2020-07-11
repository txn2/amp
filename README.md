# Admission Mutation Proxy (amp)

`amp` is a Kubernetes [Dynamic Admission Control](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/) mutating webhook **proxy** for Pods.


## Install

```shell script

```
Create Certificate as Kubernets Secret:

```shell script
curl https://raw.githubusercontent.com/morvencao/kube-mutating-webhook-tutorial/master/deployment/webhook-create-signed-cert.sh -o cert-gen.sh

chmod 775 cert-gen.sh

./cert-gen.sh --service amp --namespace amp-system --secret amp-cert
```





## Development

### Release
```bash
goreleaser --skip-publish --rm-dist --skip-validate
```

```bash
GITHUB_TOKEN=$GITHUB_TOKEN goreleaser --rm-dist
```
