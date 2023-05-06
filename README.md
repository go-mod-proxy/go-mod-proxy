![CI](https://github.com/go-mod-proxy/go-mod-proxy/workflows/ci/badge.svg)
![Docker Hub](https://img.shields.io/docker/image-size/jbrekelmans/go-module-proxy?sort=date)
[![License](https://img.shields.io/badge/License-Apache%202.0-yellowgreen.svg)](https://opensource.org/licenses/Apache-2.0)

# Introduction
A Go module proxy that:
1. Can front private repositories and supports authentication to GitHub repositories via GitHub App credentials.
1. Implements strong consistency so that `.info`, `.zip` and `.mod` always reflect the same copy of a module version across all server replicas. This is an important reliability property.
1. Uses Google Cloud Storage (see [durability and availability](https://cloud.google.com/storage/docs/storage-classes)) to realize scalable, reliable and low maintenance storage.
1. Supports client authentication and access control (but see [#2](https://github.com/go-mod-proxy/go-mod-proxy/issues/2)).

# Comparison
| | Authentication to private repositories | Persistent storage | Strong consistency | Client authentication | Community (as of 16 Sep 2023) | Caches sumdb (privacy) | Pkgsite Index |
|---|---|---|---|---|---|---|---|
| This module proxy server | - Encapsulates git over HTTPS via internal credential helper server<br/>- Least-privilege authentication to GitHub via [GitHub Apps](https://developer.github.com/apps/)<br/>- Supports only private GitHub repositories (and no other Version Control Systems) | Highly available, durable and scalable storage via Google Cloud Storage (GCS) | Yes | - Identity-based access via [Instance Identity JWTs](https://cloud.google.com/compute/docs/instances/verifying-instance-identity)<br/>- Username/password authentication<br/>- Access control lists | [11 stars](https://github.com/go-mod-proxy/go-mod-proxy) | [No, see #1](https://github.com/go-mod-proxy/go-mod-proxy/issues/1) | [No, see #228](https://github.com/go-mod-proxy/go-mod-proxy/issues/228) |
| [Athens](https://docs.gomods.io/) | Athens [documents](https://docs.gomods.io/configuration/authentication/) how to setup authentication for all (if not most) Version Control Systems, but does not encapsulate it | Supports GCS and much more | No | No | [4.1k stars](https://github.com/gomods/athens) | No | No |
| [goproxy.io](https://github.com/goproxyio/goproxy) | ? | File system | No | No | [5.5k stars](https://github.com/goproxyio/goproxy) | - | No |

# Strong consistency
The `.info`, `.zip` and `.mod` endpoints are strongly consistent. More formally:
1. Given a copy of a module version `<m>@<v>`: `.info`, `.zip` and `.mod` as reported by GET `<m>/v@/<v>.info`, `<m>/v@/<v>.zip` and `<m>/v@/<v>.mod` requests, respectively, reflect that copy and at no point (in the future) will GET `<m>/v@/<v>.info`, `<m>/v@/<v>.zip` and `<m>/v@/<v>.mod` requests reflect a different copy.
2. Given a module version `<m>@<v>`: if any one of `.info`, `.zip` and `.mod` as reported by GET `<m>/v@<v>.info`, `<m>/v@/<v>.zip` and `<m>/v@/<v>.mod` requests, respectively, reflect a copy of the module version then all future GET `<m>/v@/<v>.info`, `<m>/v@/<v>.zip` and `<m>/v@/<v>.mod` requests reflect that copy.

Similarly, list after read is strongly consistent (but list may return partial results in case of errors and list does not return pseudo-versions).

## NOTE
Strong consistency is implemented using GCS atomic object creation.For example, Amazon S3 does not support atomic object creation, but can still be pluggged in.

# Client authentication
Supports authentication using Google Compute Engine Instance Identity Tokens. This is similar to Hashicorp Vault's GCE login: https://www.vaultproject.io/docs/auth/gcp.html#gce-login.
Supports authentication via username/password.
Supports access control lists to configure fine grained access control on modules.
See the example configuration [config_example_clientauth.yaml](config_example_clientauth.yaml).
