#!/bin/bash

if [[ -z "$CLUSTER_NAME" ]]; then
    echo "Ensure CLUSTER_NAME has been set"
    exit 1
fi

if [[ -z "$COMPUTE_ZONE" ]]; then
    echo "Ensure COMPUTE_ZONE has been set"
    exit 1
fi

export ENDPOINT=$(gcloud container clusters describe $CLUSTER_NAME --zone=$COMPUTE_ZONE --format="value(endpoint)")

export CLUSTER_CA_AUTHORITY=$(gcloud container clusters describe $CLUSTER_NAME --zone=$COMPUTE_ZONE --format="value(masterAuth.clusterCaCertificate)")

export TOKEN=$(gcloud auth application-default print-access-token)

envsubst <<EOF
apiVersion: v1
kind: Config
clusters:
- name: $CLUSTER_NAME
  cluster:
    server: https://$ENDPOINT
    certificate-authority-data: $CLUSTER_CA_AUTHORITY
users:
- name: $CLUSTER_NAME
  user:
    token: $TOKEN
contexts:
- context:
    cluster: $CLUSTER_NAME
    user: $CLUSTER_NAME
  name: $CLUSTER_NAME
current-context: $CLUSTER_NAME

EOF