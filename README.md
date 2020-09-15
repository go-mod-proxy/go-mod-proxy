# Introduction
A Go module proxy that:
1. Can front private repositories (authentication to GitHub repositories using GitHub App credentials)
1. Implements strong consistency so that .info, .zip and .mod endpoints always reflects the same copy of a module version (even when running multiple replicas)
1. Uses Google Cloud Storage (see [durability and availability](https://cloud.google.com/storage/docs/storage-classes)) to support scalability and low maintenance.
1. Supports client authentication and access control

# Strong consistency
The `.info`, `.zip` and `.mod` endpoints are strongly consistent. More formally:
1. Given a copy of a module version: `.info`, `.zip` and `.mod` all reflect that copy and at no point will `.info`, `.zip` and `.mod` reflect different copies of the module version.
2. Given a module version: if any one of `.info`, `.zip` and `.mod` reflects a copy of the module version then all future `.info`, `.zip` and `.mod` reflect the same copy.

Read after list is strongly consistent:
- If list lists a module version then `.info`, `.zip` and `.mod` immediately reflect the module version.

List after read is strongly consistent.

# Client authentication
Supports authentication using Google Compute Engine Instance Identity Tokens. This is similar to Hashicorp Vault's GCE login: https://www.vaultproject.io/docs/auth/gcp.html#gce-login.
Supports authentication via username/password.
Supports access control lists to configure fine grained access control on modules.
See the example configuration [config_example_clientauth.yaml](config_example_clientauth.yaml).
