# Development

## How to run the test suite

Prerequisites:
* Go >= 1.17
* clusterctl >= 1.1.3

You can run the test suite by simply doing

```sh
make test
```

## How to run the controller locally

Ensure the cluster you are using has the CAPI CRDs installed as the controller references them. If necessary, run the `clusterctl init` command:
```sh
clusterctl init
```

Install the controller's CRDs on your test cluster:

```sh
make install
```

Run the controller locally:

```sh
make run
```