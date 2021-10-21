# Sourcecov

Sourcecov is a Kubernetes operator to manage Sourcecov Agent which collected coverage data from application during e2e test.
It requires the following edit permissions in k8s:

- role.authorization.k8s.io
- rolebinding.rbac.authorization.k8s.io
- serviceAccounts
- pods
- pods/exec
- statefulset.apps


### Generate manifests

`make print-manifests`

### Sample CR
A sample cr:

```yaml
apiVersion: sourcecov.erda.cloud/v1alpha1
kind: Agent
metadata:
  name: agent-sample
spec:
  image: nginx:stable
  env:
    - name: KEY
      value: value
  storageClassName: alicloud-disk-efficiency
  storageSize: 20Gi
  resources:
    limits:
      cpu: "0.1"
      memory: "256Mi"
    requests:
      cpu: "0.1"
      memory: "256Mi"
```