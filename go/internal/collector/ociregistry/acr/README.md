# Azure Container Registry OCI Adapter

This package owns Azure Container Registry target normalization for the OCI
registry collector. ACR repositories use `<registry>.azurecr.io` registry hosts
and the shared Distribution API client.

Azure CLI, Microsoft Entra ID, service principal, or managed identity auth is
resolved outside this package. The collector accepts environment-backed
username/password or bearer-token values and never stores credentials in facts
or metric labels.
