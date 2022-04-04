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

# Usage

### How to create a kubeconfig secret using X509 client certificates 

---
**NOTE**
This approach of authenticating to the Kubernetes API using X509 client credentials will work for unmanaged Kubernetes clusters as well as local kind clusters. Managed clusters from cloud providers such as EKS and GKE will typically require additional layers of security and the steps below will not work.

---

Requires openssl >= 1.1.1n

The following instructions explain how to create a kubeconfig that can be used to query a remote cluster from a management cluster. In the commands below, `demo-01-user` is the name of the user that will be assumed (impersonated) by the management cluster when querying the remote cluster.

1. Create a private key


   To generate a private key run the following:
   ```sh
   openssl genrsa -out demo-01-user.key 4096
   ```

2. Create a Certificate Signing Request (CSR) from the private key


   Using the key from the previous step, create a CSR by running the following:
   ```sh
   openssl req -new -key demo-01-user.key -out demo-01-user.csr -subj "/CN=demo-01-user/O=viewers"
   ```

   In the above command, CN is the name of the user and O is the group that this user will belong to. More than one groups can be specified if needed. These values will be referenced via RBAC to control access to the remote cluster.

3. Submit a CertificateSigningRequest to the remote cluster

   ```sh
   kubectl apply -f - <<EOF
   apiVersion: certificates.k8s.io/v1
   kind: CertificateSigningRequest
   metadata:
     name: demo-01-user
   spec:
     request: $(cat demo-01-user.csr | base64 | tr -d '\n')
     signerName: kubernetes.io/kube-apiserver-client
     usages:
     - client auth
   EOF
   ```

4. Approve certificate signing request

   ```sh
   kubectl certificate approve demo-01-user
   ```

5. Get the certificate

   To export the issued certificate from the CSR
   ```sh
   kubectl get csr/demo-01-user -o jsonpath='{.status.certificate}'| base64 -d > demo-01-user.crt
   ```

6. Create Role and Rolebinding (optional)

   To test that the new user can access the API successfully, add a new role/rolebinding for this user:
   
   ```sh
   kubectl create role demo-01-role --verb=get --verb=list --resource=pods
   kubectl create rolebinding demo-01-role-binding --role=demo-01-role --user=demo-01-user
   ```
7. Get the CA certificate of your cluster

   To get the CA certificate for a kind cluster, first find the container ID of the control plane by running `docker ps`. Then run the following command substituting the container ID:
   ```sh
   docker cp <container-id>:/etc/kubernetes/pki/ca.crt .
   ```

   For other types of clusters you will usually need to copy the file from your cluster using SSH or download it from a console UI.

8. Create a kubeconfig

    Create a YAML file by running the helper script `./x509/static-kubeconfig`:

    ```sh
    CLUSTER_NAME=demo-01 \
    USER=demo-01-user \
    CA_CERTIFICATE=ca.crt \
    ENDPOINT=demo-01-control-plane:6443 \
    CLIENT_CERTIFICATE=demo-01-user.crt \
    CLIENT_KEY=demo-01-user.key \
    ./tools/x509/static-kubeconfig.sh > demo-01-kubeconfig
    ```

    Replace the following:
    - CLUSTER_NAME: the name of your cluster i.e. `demo-01`
    - ENDPOINT: the API server endpoint i.e. `demo-01-control-plane:6443`
    - CA_CERTIFICATE: path to the CA certificate file of the cluster
    - USER: the name of the user i.e. `demo-01-user`
    - CLIENT_CERTIFICATE: path to the client certificate file from step 5 above i.e. the path to the `demo-01-user.crt` file
    - CLIENT_KEY: path to the private key file from step 1 above i.e. the path to the `demo-01-user.key` file

    Finally create a secret for the generated kubeconfig:

    ```sh
    kubectl create secret generic demo-01-kubeconfig \
    --from-file=value.yaml=./demo-01-kubeconfig \
    --dry-run=client -o yaml > demo-01-kubeconfig.yaml
    ```

---
**NOTES FOR KIND CLUSTERS**

- To get the endpoint of the API server of a kind cluster run the following command:
```sh
kind get kubeconfig --name demo-01 --internal | yq '.clusters[0].cluster.server'
https://demo-01-control-plane:6443
```
The domain name and port returned (`demo-01-control-plane:6443` in the above example) is the endpoint of the API server.

---

### How to test a kubeconfig secret in a cluster

To test a kubeconfig secret has been correctly setup apply the following manifest and check the logs after the job completes:

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: kubectl
spec:
  ttlSecondsAfterFinished: 30
  template:
    spec:
      containers:
      - name: kubectl
        image: bitnami/kubectl
        args: [ "get", "pods", "-n", "kube-system", "--kubeconfig", "/etc/kubeconfig/value.yaml"]
        volumeMounts:
        - name: kubeconfig
          mountPath: "/etc/kubeconfig"
          readOnly: true
      restartPolicy: Never
      volumes:
      - name: kubeconfig
        secret:
          secretName: demo-01-kubeconfig
          optional: false
```

In the manifest above `demo-01-kubeconfig`is the name of the secret that contains the kubeconfig for the remote cluster.

---
# Background
- [Authentication strategies](https://kubernetes.io/docs/reference/access-authn-authz/authentication/#authentication-strategies)
  - [X509 client certificates](https://kubernetes.io/docs/reference/access-authn-authz/authentication/#x509-client-certs): can be used across different namespaces
  - [Service account tokens](https://kubernetes.io/docs/reference/access-authn-authz/authentication/#service-account-tokens): limited to a single namespace
- [Kubernetes authentication 101 (CNCF blog post)](https://www.cncf.io/blog/2020/07/31/kubernetes-rbac-101-authentication/)
- [Kubernetes authentication (Magalix blog post)](https://www.magalix.com/blog/kubernetes-authentication)