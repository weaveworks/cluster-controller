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

8. Create a kubeconfig secret

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

### How to create a kubeconfig secret for GKE using Terraform

---
**NOTE**
This approach uses the `google_client_config` resource from the official Google Cloud Terraform provider to retrieve an access token that is valid for 1 hour. Therefore this method of creating a kubeconfig secret can be used in dev environments but is not recommended for production environments.

---

Requires terraform >= 1.1.5

The following instructions explain how to create a kubeconfig that can be used to query a remote GKE cluster from a management cluster. It assumes that you have already created a GKE cluster through Terraform and want to generate a kubeconfig secret for it.

1. Add the following template to your Terraform configuration

```yaml
apiVersion: v1
kind: Config

clusters:
- name: ${cluster_name}
  cluster:
    server: https://${endpoint}
    certificate-authority-data: ${cluster_ca_certificate}

users:
- name: ${user_name}
  user:
    token: ${token}

contexts:
- name: ${context}
  context:
    cluster: ${cluster_name}
    user: ${user_name}
  
current-context: ${context}
```

The template parameters `${..}` will be substituted in the next step.

2. Add the following data sources to your Terraform configuration

```tf
data "google_client_config" "provider" {}

data "template_file" "kubeconfig" {
  template = file("${path.module}/templates/kubeconfig.yaml.tpl")

  vars = {
    cluster_name            = <cluster-name>
    user_name               = <user-name>
    context                 = <cluster-name>
    cluster_ca_certificate  = <cluster-ca-certificate>
    // The cluster's Kubernetes API endpoint
    endpoint                = <cluster-endpoint>
    // The OAuth2 access token used by the client to authenticate against the Google Cloud API.
    token                   = data.google_client_config.provider.access_token
  }
}
```

The `kubeconfig` data source references the template created in step 1 and populates its parameters. The values will typically be set to outputs of a cluster resource or module. The token will belong to the user running the Terraform commands.

3. Add an output value for the kubeconfig to your Terraform cluster configuration

```tf
output "demo_01_kubeconfig" {
  description = "A kubeconfig file configured to access the GKE cluster."
  value       = data.template_file.kubeconfig.rendered
}
```

4. Create the kubeconfig

Run the following command to output the kubeconfig

```sh
terraform output -raw demo_01_kubeconfig > demo-01-kubeconfig
```
Finally create a secret for the generated kubeconfig:

```sh
kubectl create secret generic demo-01-kubeconfig \
--from-file=value.yaml=./demo-01-kubeconfig \
--dry-run=client -o yaml > demo-01-kubeconfig.yaml
```

### How to create a kubeconfig secret for GKE using gcloud

---
**NOTE**
This approach uses `gcloud` to retrieve an access token that is valid for 1 hour. Therefore this method of creating a kubeconfig secret can be used in dev environments but is not recommended for production environments.

---

Requires gcloud >= 352.0.0

1. List your running clusters and their locations using `gcloud`

```sh
gcloud container clusters list --format="table(name,zone)" --filter="status=running"
NAME                                    LOCATION
demo-01                                 europe-north1-a
```

2. Run the following command to output the kubeconfig

```sh
CLUSTER_NAME=demo-01 COMPUTE_ZONE=europe-north1-a ./tools/gke/token-kubeconfig.sh > demo-01-kubeconfig
```

Replace the following:
- CLUSTER_NAME: the name of your cluster i.e. `demo-01`
- COMPUTE_ZONE: the GCP location (zone) of your cluster i.e. `europe-north1-a`

Finally create a secret for the generated kubeconfig:

```sh
kubectl create secret generic demo-01-kubeconfig \
--from-file=value.yaml=./demo-01-kubeconfig \
--dry-run=client -o yaml > demo-01-kubeconfig.yaml
```

### How to create a kubeconfig secret using a service account

1. Create a new service account on the remote cluster:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: demo-01
  namespace: default
```

2. Add RBAC permissions for the service account

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: pods-reader
  namespace: default
rules:
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get", "watch", "list"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: read-pods
  namespace: default
subjects:
- kind: ServiceAccount
  name: demo-01
  namespace: default
roleRef:
   kind: Role
   name: pods-reader
   apiGroup: rbac.authorization.k8s.io
```

In this example, `demo-01` can list the pods in the default namespace.

3. Get the token of the service account

First get the list of secrets of the service accounts by running the following command:
```sh
kubectl get secrets --field-selector type=kubernetes.io/service-account-token
NAME                      TYPE                                  DATA   AGE
default-token-lsjz4       kubernetes.io/service-account-token   3      13d
demo-01-token-gqz7p       kubernetes.io/service-account-token   3      99m
```
`demo-01-token-gqz7p` is the secret that holds the token for `demo-01` service account

To get the token of the service account run the following command:
```sh
TOKEN=$(kubectl get secret demo-01-token-gqz7p -o jsonpath={.data.token} | base64 -d)
```

4. Create a kubeconfig secret

For the next step, the cluster certificate (CA) is needed. How you get hold of the certificate depends on the cluster. For GKE you can view it on the GCP Console:  Cluster->Details->Endpoint->”Show cluster certificate”. You will need to copy the contents of the certificate into the `ca.crt` file used below. 

```sh
CLUSTER_NAME=demo-01 \
CA_CERTIFICATE=ca.crt \
ENDPOINT=<control-plane-ip-address> \
TOKEN=<token> ./tools/sa/static-kubeconfig.sh > demo-01-kubeconfig
```

Replace the following:
- CLUSTER_NAME: the name of your cluster i.e. `demo-01`
- ENDPOINT: the API server endpoint i.e. `34.218.72.31`
- CA_CERTIFICATE: path to the CA certificate file of the cluster
- TOKEN: the token of the service account retrieved in the previous step

Finally create a secret for the generated kubeconfig:

```sh
kubectl create secret generic demo-01-kubeconfig \
--from-file=value.yaml=./demo-01-kubeconfig \
--dry-run=client -o yaml > demo-01-kubeconfig.yaml
```

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