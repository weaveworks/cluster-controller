#!/bin/bash

if [[ -z "$CA_CERTIFICATE" ]]; then
    echo "Ensure CA_CERTIFICATE has been set to the path of the CA certificate"
    exit 1
fi

if [[ -z "$ENDPOINT" ]]; then
    echo "Ensure ENDPOINT has been set"
    exit 1
fi

if [[ -z "$CLIENT_CERTIFICATE" ]]; then
    echo "Ensure CLIENT_CERTIFICATE has been set to the path of the client certificate"
    exit 1
fi

if [[ -z "$CLIENT_KEY" ]]; then
    echo "Ensure CLIENT_KEY has been set to the path of the client certificate key"
    exit 1
fi

export CLUSTER_CA_CERTIFICATE=$(cat "$CA_CERTIFICATE" | base64)

export CLIENT_CERTIFICATE=$(cat "$CLIENT_CERTIFICATE" | base64)

export CLIENT_KEY=$(cat "$CLIENT_KEY" | base64)

envsubst <<EOF
apiVersion: v1
kind: Config
clusters:
- name: $CLUSTER_NAME
  cluster:
    server: https://$ENDPOINT
    certificate-authority-data: $CLUSTER_CA_CERTIFICATE
users:
- name: $USER
  user:
    client-certificate-data: $CLIENT_CERTIFICATE
    client-key-data: $CLIENT_KEY
contexts:
- name: $CLUSTER_NAME
  context:
    cluster: $CLUSTER_NAME
    user: $USER
current-context: $CLUSTER_NAME

EOF