# Kubernetes Manifest Deployer

The kubernetes manifest deployer is a controller that reconciles DeployItems of type `landscaper.gardener.cloud/kubernetes-manifest`.
It deploys the configured kubernetes manifest into the target cluster.

It also checks by default the health of the deployed resources. See [healthchecks.md](healthchecks.md) for more info.

**Index**:
- [Provider Configuration](#provider-configuration)
- [Provider Status](#status)
- [Deployer Configuration](#deployer-configuration)

### Provider Configuration

This sections describes the provider specific configuration

```yaml
apiVersion: landscaper.gardener.cloud/v1alpha2
kind: DeployItem
metadata:
  name: my-manifests
spec:
  type: landscaper.gardener.cloud/kubernetes-manifest

  target: # has to be of type landscaper.gardener.cloud/kubernetes-cluster
    name: my-cluster
    namespace: test

  config:
    apiVersion: manifest.deployer.landscaper.gardener.cloud/v1alpha2
    kind: ProviderConfiguration

    updateStrategy: update | patch | merge | mergeOverwrite # optional; defaults to update

    # Configuration of the readiness checks for the resources.
    # optional
    readinessChecks:
      # Allows to disable the default readiness checks.
      # optional; set to false by default.
      disableDefault: true
      # Defines the time to wait before giving up on a resource
      # to be ready. Should be changed with long startup time pods.
      # optional; default to 180 seconds/3 minutes.
      timeout: 3m
      # Configuration of custom readiness checks which are used
      # to check on custom fields and their values
      # especially useful for resources that came in through CRDs
      # optional
      custom:
      # the name of the custom readiness check, required
      - name: myCustomReadinessCheck
        # timeout of the custom readiness check
        # optional, defaults to the timeout stated above
        timeout: 2m
        # temporarily disable this custom readiness check, useful for test setups
        # optional, defaults to false
        disabled: false
        # a specific resource should be selected for this readiness check to be performed on
        # a resource is uniquely defined by its GVK, namespace and name
        # required if no labelSelector is specified, can be combined with a labelSelector which is potentially harmful
        resourceSelector:
          - apiVersion: apps/v1
            kind: Deployment
            name: myDeployment
            namespace: myNamespace
        # multiple resources for the readiness check to be performed on can be selected through labels
        # they are identified by their GVK and a set of labels that all need to match
        # required if no resourceSelector is specified, can be combined with a resourceSelector which is potentially harmful
        labelSelector:
          apiVersion: apps/v1
          kind: Deployment
          matchLabels:
            app: myApp
            component: backendService
        # requirements specifies what condition must hold true for the given objects to pass the readiness check
        # multiple requirements can be given and they all need to successfully evaluate
        requirements:
        # jsonPath denotes the path of the field of the selected object to be checked and compared
        - jsonPath: .status.readyReplicas
          # operator specifies how the contents of the given field should be compared to the desired value
          # allowed operators are: DoesNotExist(!), Exists(exists), Equals(=, ==), NotEquals(!=), In(in), NotIn(notIn)
          operator: In
          # values is a list of values that the field at jsonPath must match to according to the operators
          values:
          - value: 1
          - value: 2
          - value: 3

    # Defines the time to wait before giving up on a resource to be deleted,
    # for instance when deleting resources that are not anymore managed from this DeployItem.
    # optional; default to 180 seconds/3 minutes.
    deleteTimeout: 2m

    manifests: # list of kubernetes manifests
    - policy: manage | fallback | ignore | keep | immutable
      # Optional: A map of annotations that are only added to the manifest when it is first created on the target.
      # These annotations are not getting re-applied during an update of the manifest.
      annotateBeforeCreate:
        annotationA: valueA
        annotationB: valueB
      # Optional: A map of annotations that are only added to the manifest when before it is being deleted on the target.
      annotateBeforeDelete:
        annotationA: valueA
        annotationB: valueB
      # the manifest specification
      manifest:
        apiVersion: v1
        kind: Secret
        metadata:
          name: my-secret
          namespace: default
        data:
          config: abc
    - ...
    
    # Define exports that are read from the kubernetes resources,
    # so they can be used by other deployitems or installations.
    # The deployer tries to read the export values until either the global or the specific timeout is exceeded.
    exports:
      defaultTimeout: 5m # global default timeout that is used when no specific timeout is set
      exports:
      - key: KeyA # value is read from a secret and exported with name "KeyA"
        timeout: 10m # optional specific timeout
        jsonPath: .data.somekey # points to the value in the resource that is being exported
        fromResource: # required
          apiVersion: v1 # specification of the resource type
          kind: Secret
          name: my-secret # name of the resource
          namespace: a # namespace of the resource
      - key: KeyB # value is read from secret that is referenced by a service account and exported with name "KeyB"
        timeout: 10m # optional specific timeout
        jsonPath: .secrets[0] # points to an object reference that consists of a name and namespace
        fromResource:
          apiVersion: v1 # specification of the resource type
          kind: ServiceAccount
          name: my-user # name of the resource
          namespace: a # namespace of the resource
        # Defines the referenced objects kind and version. 
        # The name and namespace is taken from the jsonPath defined in "fromResource".
        fromObjectRef:
          apiVersion: v1
          kind: Secret
          jsonPath: ".data.somekey" # points to the value in the resource that is being exported
```
__Update Strategy__:

The update strategy defines the behavior of the manifest deployer when a resource for a rendered manifest already exists on the target cluster.

- `update`: The resources on the cluster will be updated with the results of the rendered manifests (default). Any changes to the resources, applied externally on the cluster, may be lost after the update.
- `patch`: The manifest deployer will calculate a JSON diff between the resources on the cluster and the rendered manifests. The diff will be applied as a patch. Any changes to the resources, applied externally on the cluster, may be lost after the update.
- `merge`: The manifest deployer will merge the results of the rendered manifests into the resources on the cluster. Fields that already exist in the resources on the cluster, will not be overwritten.
- `mergeOverwrite`: The manifest deployer will merge the results of the rendered manifests into the resources on the cluster. Fields that already exist in the resources on the cluster, will be overwritten when the rendered field is not empty.

__Policy__:

- `manage`: The manifest will be created, updated and deleted (occupies already managed resources).
- `fallback`: The manifest will be created, updated and deleted (only if not already managed by someone else: check for annotation with landscaper identity, deployitem name + namespace)
- `keep`: The manifest will be created, updated, but not deleted.
- `ignore`: The manifest will be completely ignored.
- `immutable`: The manifest will be created and deleted, but never updated. 

### Status

This section describes the provider specific status of the resource

```yaml
status:
  providerStatus:
    apiVersion: manifest.deployer.landscaper.gardener.cloud
    kind: ProviderStatus
    managedResources:
    - apiGroup: k8s.apigroup.com/v1
      kind: my-type
      name: my-resource
      namespace: default
```

## Deployer Configuration

When deploying the manifest deployer controller it can be configured using the `--config` flag and providing a configuration file.

The structure of the provided configuration file is defined as follows.

:warning: Keep in mind that when deploying with the helm chart the configuration is abstracted using the helm values. See the [helm values file](../../charts/manifest-deployer/values.yaml) for details when deploying with the helm chart.
```yaml
apiVersion: manifest.deployer.landscaper.gardener.cloud/v1alpha1
kind: Configuration

# target selector to only react on specific deploy items.
# see the common config in "./README.md" for detailed documentation.
targetSelector:
  annotations: []
  labels: []
```
