# knative-ci

Example CI system built using knative

## Dependencies

These must be installed in order to build and run knative-ci

* [knative/build](https://github.com/knative/build)
* [knative/serving](https://github.com/knative/serving)
* [ko](https://github.com/google/go-containerregistry/tree/master/cmd/ko)


## Build and Install

The [ko](https://github.com/google/go-containerregistry/tree/master/cmd/ko) utility is used for development.

To build and install knative-ci:

```shell
ko apply -f config
```

## Design

Users of knative-ci define a series of "steps" in yaml files. These steps
map to the steps of a knative/build BuildTemplate. When CI is triggered on a
repository we first generate a BuildTempalte for that repository and define
it. We then execute a build setting the proper repository properties as input.

## Secrets

Kubernetes secrets are used for the storage of secrets (e.g. docker registry
auth). This requires someone with access to the kubernetes cluster to add the
secrets to the cluster as described in https://github.com/knative/docs/blob/master/build/auth.md

A user can then define `serviceAccountName` as part of their build template.
