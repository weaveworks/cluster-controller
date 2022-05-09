# Changelog

## [1.1.0](https://github.com/weaveworks/cluster-controller/compare/v1.0.0...v1.1.0) (2022-05-09)


### Features

* add logic for making sure cluster is extra ready ([71a6cc7](https://github.com/weaveworks/cluster-controller/commit/71a6cc728bb9c41deef48528d98790ca82aaa75f))


### Bug Fixes

* use CAPIClusterRef when looking for kubeconfig secret ([4191177](https://github.com/weaveworks/cluster-controller/commit/4191177a12f6139e3eab3566b02e8be916adafea))
* use the NewGitopsClusterReconciler constructor ([a472d8c](https://github.com/weaveworks/cluster-controller/commit/a472d8c3d047691180d7fb6fc4710e3c79ec48e9))
* wait for control plane for readiness ([7d9497f](https://github.com/weaveworks/cluster-controller/commit/7d9497f4eea5714bd3b8b207559d179a4e4598a2))

## 1.0.0 (2022-04-21)


### Features

* Implement reconcile logic ([e846068](https://github.com/weaveworks/cluster-controller/commit/e846068db9ddd1132e635a78f5aa067a5cca90e7))


### Bug Fixes

* Dont error when the cluster secret doesn't exist. ([b3cd729](https://github.com/weaveworks/cluster-controller/commit/b3cd7294b42152eff69598ea305ec941d5e6737b))
