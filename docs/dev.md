# Development

This document covers the first time setup for developing Monax in a Linux
environment. The following dependencies are required for building and testing,
including the minimum version tested to work:

Programming:

* [Golang](https://go.dev/) (v1.23.4+)
* [Proto Compiler](https://protobuf.dev/) (`protoc --version`: libprotoc 33.5+)

Images and Cluster Management:

* [Docker](https://www.docker.com/) (v29.1.3+)
* [kubectl](https://kubernetes.io/docs/reference/kubectl/) (v1.33.5+)
* [kind](https://kind.sigs.k8s.io/) (v0.30.0+)

## Programming

These are the requirements to work with the codebase.

### Install Golang

In brief, read https://go.dev/doc/install/ for installation and documentation.

Once installed, ensure that `go version` returns a version equal to or greater
than the go version in [go.mod](go.mod).

### Protobuf Compiling

This section is only needed to make changes to `*.proto` files. Otherwise, the
previously compiled files will work.

Golang does not natively know how to directly use protobuf files. Instead, the
`go generate` command is used to convert those files into `proto.pb.go`. This
conversion is done through a binary called `protoc`. However, the managed
package installations of this tool include a version that is too old
(`libprotoc 3.21.12`) to work with the `editions` syntax.

To install the newest version, go to https://protobuf.dev/installation/ and
follow the directions for **Install Pre-compiled Binaries (Any OS)**.

Next, run the following commands to install the gRPC and protobuf extensions to
`protoc`:

```shell
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
```

Verify the tools were installed correctly:

```shell
which -s protoc protoc-gen-go protoc-gen-go-grpc || echo "could not find all protoc tools"
```

If the install worked, the command will not print anything and commands like
`go generate ./...` will succeed.

## Images and Cluster Management

These are the requirements to run integration tests of SUTs with Monax.

### Install Docker

Docker is used to build OCI images to be used in the local Kubernetes cluster.

> ⓘ **NOTE**: This will install version `29.1.3` which was known to work with
> Monax at release time. You can instead install a newer version if you need new
> features or are having problems.

1. Follow the installation
   [instructions](https://docs.docker.com/engine/install/debian)

2. Follow the [post-installation
   steps](https://docs.docker.com/engine/install/linux-postinstall/) for Linux,
   specifically [Manage Docker as a non-root
   user](https://docs.docker.com/engine/install/linux-postinstall/#manage-docker-as-a-non-root-user)

   ```shell
   sudo groupadd docker
   sudo usermod -aG docker $USER
   ```

### Install Kubectl

The `kubectl` binary is used for manual probing of the local Kubernetes cluster.

> ⓘ **NOTE**: This will install the latest `stable` version. If you're unable
> to install the latest, use at least version `1.33.5` which was known to work
> with kind at release time.

1. Follow the installation:
   [instructions](https://kubernetes.io/docs/tasks/tools/install-kubectl-linux/)

```shell
curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"
sudo install -o root -g root -m 0755 kubectl /usr/local/bin/kubectl
```

### Install kind

The `kind` binary is used to manage the local Kubernetes cluster.

> ⓘ **NOTE**: This will install version `0.30.0` which was known to work with
> Monax at release time. You can instead install a newer version if you need new
> features or are having problems.

```shell
go install sigs.k8s.io/kind@v0.30.0
```

## Setup the local Kubernetes cluster

Monax can use the same basic kind cluster setup as
[openconfig/kne](https://github.com/openconfig/kne/). You can find their setup
directions in their
[create_topology.md](https://github.com/openconfig/kne/blob/49957c5046cfc122a08039c4bf687a8e1fc7ec86/docs/create_topology.md?plain=1#L156-L166).
